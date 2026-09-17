package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/app"
	"github.com/cRolandoJr/ailoop/internal/cli"
	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/pterm/pterm"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	cwd, _ := os.Getwd()

	switch command {
	case "start":
		if len(os.Args) < 3 {
			fmt.Println("Error: 'start' requires a task description")
			fmt.Println("Usage: ailoop start \"Task description\"")
			os.Exit(1)
		}
		task := os.Args[2]
		s := state.NewState(task)
		if err := state.Save(cwd, s); err != nil {
			pterm.Error.Printf("Failed to save state: %v\n", err)
			os.Exit(1)
		}

		pterm.DefaultHeader.WithFullWidth().Println("AI Loop Engine")
		pterm.Success.Printf("Started new context for task: %s\n", task)

		// The project declares what "verified" means for it. Without this file
		// there is no gate, and DONE would be nothing but the user clicking yes.
		if _, err := config.Load(cwd); err == config.ErrNotFound {
			c := config.Detect(cwd)
			if err := config.Save(cwd, c); err != nil {
				pterm.Warning.Printf("Could not write verification config: %v\n", err)
			} else if len(c.Verify) == 0 {
				pterm.Warning.Printf("No stack detected. Declare your verification commands in %s\n", config.Path(cwd))
				pterm.Warning.Println("Until you do, this loop cannot reach DONE.")
			} else {
				pterm.Info.Printf("Verification config written to %s (%d checks)\n", config.Path(cwd), len(c.Verify))
			}
		}

		pterm.Info.Println("Run 'ailoop next' to begin Discovery.")

	case "next":
		nextCmd := flag.NewFlagSet("next", flag.ExitOnError)
		filesFlag := nextCmd.String("files", "", "Comma-separated list of files to point the agent at")
		criticFlag := nextCmd.Bool("critic", false, "Enable adversarial critic engine for this phase")
		inlineFlag := nextCmd.Bool("inline", false, "Paste --files contents into the prompt instead of letting the agent read them")
		if err := nextCmd.Parse(os.Args[2:]); err != nil {
			cli.Error(err)
			os.Exit(1)
		}

		ctx := context.Background()
		loop, pool, err := buildLoop(ctx, cwd)
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		if pool != nil {
			defer pool.Close()
		}

		var files []string
		if *filesFlag != "" {
			files = strings.Split(*filesFlag, ",")
		}
		pc, err := loop.BuildProjectContext(files, *inlineFlag)
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.ProjectContext(pc)

		out, err := loop.Advance(ctx, app.AdvanceOptions{
			ProjectContext: pc.Text,
			UseCritic:      *criticFlag,
			Review:         cli.Review,
			OnTool:         cli.ToolUsed,
			OnProgress:     cli.Progress,
		})
		if err != nil {
			var ge *app.GuardError
			if errors.As(err, &ge) {
				cli.GuardRefusal(ge)
			} else {
				cli.Error(err)
			}
			os.Exit(1)
		}

		ledger, _ := loop.Cost()
		cli.Outcome(out, ledger)

	case "status":
		r, err := newLoop(cwd, nil).Status()
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.Status(r)

	case "undo":
		out, err := newLoop(cwd, nil).Undo()
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.UndoOutcome(out)

	case "mcp":
		cfg, err := config.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		if len(cfg.MCP) == 0 {
			pterm.Info.Printf("No MCP servers configured. Add them under \"mcp\" in %s\n", config.Path(cwd))
			break
		}
		pool := mcp.Open(context.Background(), cfg.MCP)
		defer pool.Close()
		for name, e := range pool.Failed() {
			pterm.Error.Printf("%s: %v\n", name, e)
		}
		if pool.Empty() {
			pterm.Warning.Println("No tools available.")
			break
		}
		pterm.Success.Printf("%d tools available:\n", pool.Count())

		catalog := pool.Catalog()
		if len(os.Args) > 2 && os.Args[2] == "--schemas" {
			fmt.Print(pool.Schemas())
		} else {
			fmt.Print(catalog)
		}

		// What the deferred design is worth, in the only unit that matters here.
		schemas := pool.Schemas()
		pterm.Info.Printf("\ncatalog sent to the model: %d bytes (~%d tokens)\n", len(catalog), len(catalog)/4)
		pterm.Info.Printf("full schemas would be:     %d bytes (~%d tokens)\n", len(schemas), len(schemas)/4)
		if len(schemas) > len(catalog) {
			pterm.Info.Printf("saved on every request:    ~%d tokens (%.0f%%)\n",
				(len(schemas)-len(catalog))/4,
				100*float64(len(schemas)-len(catalog))/float64(len(schemas)))
		}

	case "cost":
		ledger, err := newLoop(cwd, nil).Cost()
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.Cost(ledger)

	case "capabilities":
		r, err := newLoop(cwd, getLLMClient()).Capabilities()
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.Capabilities(r)

	case "decide":
		if len(os.Args) < 3 {
			fmt.Println("Usage: ailoop decide \"statement\" [rationale]")
			os.Exit(1)
		}
		rationale := ""
		if len(os.Args) > 3 {
			rationale = os.Args[3]
		}
		d, err := newLoop(cwd, nil).Decide(os.Args[2], rationale)
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.DecisionRecorded(d)

	case "decisions":
		ds, err := newLoop(cwd, nil).Decisions()
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.Decisions(ds)

	case "history":
		hs, err := newLoop(cwd, nil).History()
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.History(hs)

	case "verify":
		loop, pool, err := buildLoop(context.Background(), cwd)
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		if pool != nil {
			defer pool.Close()
		}
		report, err := loop.Verify(context.Background())
		if err != nil {
			cli.Error(err)
			os.Exit(1)
		}
		cli.Verification(report)
		if !report.Passed {
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func getLLMClient() llm.Client {
	// Anthropic first: it is the only adapter here that reports vision and
	// thinking as established rather than unknown.
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return llm.NewClaudeClient(key, os.Getenv("ANTHROPIC_MODEL"))
	}

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		geminiModel := os.Getenv("GEMINI_MODEL")
		if geminiModel == "" {
			geminiModel = "gemini-1.5-pro"
		}
		return &llm.GeminiClient{
			APIKey: geminiKey,
			Model:  geminiModel,
			HTTP:   &http.Client{Timeout: 60 * time.Second},
		}
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "llama3"
	}

	return &llm.OpenAIClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func printUsage() {
	fmt.Println("AI Loop CLI - Workflow Engine for LLMs")
	fmt.Println("Usage:")
	fmt.Println("  ailoop start \"Task description\"   - Start a new work context")
	fmt.Println("  ailoop next [--files f1.go,f2.go] - Proceed to the next phase")
	fmt.Println("  ailoop next --inline             - ...pasting those files in full instead of letting the agent read them")
	fmt.Println("  ailoop undo                     - Revert to the previous phase")
	fmt.Println("  ailoop status                   - Show current state")
	fmt.Println("  ailoop verify                   - Run the project's own verification commands")
	fmt.Println("  ailoop capabilities             - Show what the selected model can do, and what each phase may use")
	fmt.Println("  ailoop mcp [--schemas]          - List the tools exposed by configured MCP servers")
	fmt.Println("  ailoop cost                     - Token spend so far, per phase")
	fmt.Println("  ailoop decide \"what\" [why]      - Record an approved decision")
	fmt.Println("  ailoop decisions                - List the Decision Record")
	fmt.Println("  ailoop history                  - Show archived revisions of each artifact")
}

// geminiModel lets the model be configured instead of frozen in the binary.
func geminiModel() string {
	if m := os.Getenv("GEMINI_MODEL"); m != "" {
		return m
	}
	return "gemini-2.5-pro"
}

// newLoop is the composition root for the read-only commands: it wires the
// dependencies a use case needs and hands back something testable. The heavy
// commands build their own with the MCP pool and config attached.
func newLoop(workspace string, client llm.Client) *app.Loop {
	cfg, _ := config.Load(workspace)
	return app.NewLoop(workspace, client, nil, cfg)
}

// buildLoop is the composition root for the commands that need a model and
// external tools. It wires the dependencies; everything it builds is injected,
// so the same use case runs under a test with fakes.
func buildLoop(ctx context.Context, workspace string) (*app.Loop, *mcp.Pool, error) {
	cfg, err := config.Load(workspace)
	if err != nil && err != config.ErrNotFound {
		return nil, nil, err
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	var pool *mcp.Pool
	if len(cfg.MCP) > 0 {
		pool = mcp.Open(ctx, cfg.MCP)
		for name, e := range pool.Failed() {
			pterm.Warning.Printf("MCP server %q unavailable: %v\n", name, e)
		}
		if n := pool.Count(); n > 0 {
			pterm.Info.Printf("%d external tools available via MCP\n", n)
		}
	}

	return app.NewLoop(workspace, getLLMClient(), pool, cfg), pool, nil
}
