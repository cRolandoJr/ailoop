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
	"github.com/cRolandoJr/ailoop/internal/env"
	"github.com/cRolandoJr/ailoop/internal/host"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/pterm/pterm"
)

func main() {
	os.Exit(run(os.Args))
}

// run is the real entry point, returning an exit code instead of calling
// os.Exit from inside a command.
//
// os.Exit does not run deferred functions, and one of those closes the MCP
// server processes: exiting from a case left them running. It also lets the
// dashboard invoke a command without main calling itself.
func run(args []string) int {
	cwd, _ := os.Getwd()

	// Once per invocation, and loudly. Migration used to happen as a side
	// effect of asking where the state lived, with every error discarded.
	if moved, err := env.Migrate(cwd); err != nil {
		cli.Error(fmt.Errorf("could not migrate the loop state: %w", err))
		return 1
	} else if moved {
		pterm.Info.Println("Loop state migrated to this branch's directory.")
	}

	if len(args) < 2 {
		return runDashboard(cwd)
	}

	command := args[1]

	switch command {
	case "start":
		startCmd := flag.NewFlagSet("start", flag.ExitOnError)
		budgetUSD := startCmd.Float64("budget", 0, "abort the work item above this estimated cost in USD (needs prices in the config)")
		budgetTok := startCmd.Int("max-tokens", 0, "abort the work item above this many tokens")
		if err := startCmd.Parse(args[2:]); err != nil {
			cli.Error(err)
			return 1
		}
		if startCmd.NArg() > 0 {
			args = append(args[:2], startCmd.Args()...)
		}
		if len(args) < 3 {
			fmt.Println("Error: 'start' requires a task description")
			fmt.Println("Usage: ailoop start \"Task description\"")
			return 1
		}
		task := args[2]
		s := state.NewState(task)

		// The project's ceiling by default; the flags lower it for this run.
		if cfg, err := config.Load(cwd); err == nil {
			s.Budget = cfg.Budget
		}
		if *budgetTok > 0 {
			s.Budget.MaxTokens = *budgetTok
		}
		if *budgetUSD > 0 {
			s.Budget.MaxUSD = *budgetUSD
		}
		if err := state.Save(cwd, s); err != nil {
			pterm.Error.Printf("Failed to save state: %v\n", err)
			return 1
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

		if s.Budget.Set() {
			pterm.Info.Printf("Budget: %s\n", s.Budget.Remaining(&s.Spend))
		}
		pterm.Info.Println("Run 'ailoop next' to begin Discovery.")

	case "next":
		nextCmd := flag.NewFlagSet("next", flag.ExitOnError)
		filesFlag := nextCmd.String("files", "", "Comma-separated list of files to point the agent at")
		criticFlag := nextCmd.Bool("critic", false, "Enable adversarial critic engine for this phase")
		inlineFlag := nextCmd.Bool("inline", false, "Paste --files contents into the prompt instead of letting the agent read them")
		if err := nextCmd.Parse(args[2:]); err != nil {
			cli.Error(err)
			return 1
		}

		ctx := context.Background()
		loop, pool, err := buildLoop(ctx, cwd)
		if err != nil {
			cli.Error(err)
			return 1
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
			return 1
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
			return 1
		}

		ledger, _, _ := loop.Cost()
		cli.Outcome(out, ledger)

	case "watch":
		ctx := context.Background()
		loop, pool, err := buildLoop(ctx, cwd)
		if err != nil {
			cli.Error(err)
			return 1
		}
		if pool != nil {
			defer pool.Close()
		}

		if err := loop.Watch(ctx, app.AdvanceOptions{
			Review:     cli.Review,
			OnTool:     cli.ToolUsed,
			OnProgress: cli.Progress,
		}); err != nil {
			cli.Error(err)
			return 1
		}

	case "status":
		r, err := newLoop(cwd, nil).Status()
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.Status(r)

	case "undo":
		out, err := newLoop(cwd, nil).Undo()
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.UndoOutcome(out)

	case "mcp":
		cfg, err := config.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			return 1
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
		if len(os.Args) > 2 && args[2] == "--schemas" {
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
		ledger, budget, err := newLoop(cwd, nil).Cost()
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.Cost(ledger, budget)

	case "doctor":
		cli.Doctor(host.Discover())

	case "capabilities":
		r, err := newLoop(cwd, getLLMClient()).Capabilities()
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.Capabilities(r)

	case "decide":
		if len(args) < 3 {
			fmt.Println("Usage: ailoop decide \"statement\" [rationale]")
			return 1
		}
		rationale := ""
		if len(args) > 3 {
			rationale = args[3]
		}
		d, err := newLoop(cwd, nil).Decide(args[2], rationale)
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.DecisionRecorded(d)

	case "decisions":
		ds, err := newLoop(cwd, nil).Decisions()
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.Decisions(ds)

	case "history":
		hs, err := newLoop(cwd, nil).History()
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.History(hs)

	case "verify":
		loop, pool, err := buildLoop(context.Background(), cwd)
		if err != nil {
			cli.Error(err)
			return 1
		}
		if pool != nil {
			defer pool.Close()
		}
		report, err := loop.Verify(context.Background())
		if err != nil {
			cli.Error(err)
			return 1
		}
		cli.Verification(report)
		if !report.Passed {
			return 1
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		return 1
	}

	return 0
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
	fmt.Println("  ailoop start [--budget=0.50] [--max-tokens=N] \"Task\" - Start a new work context")
	fmt.Println("  ailoop next [--files f1.go,f2.go] - Proceed to the next phase")
	fmt.Println("  ailoop next --inline             - ...pasting those files in full instead of letting the agent read them")
	fmt.Println("  ailoop undo                     - Revert to the previous phase")
	fmt.Println("  ailoop status                   - Show current state")
	fmt.Println("  ailoop verify                   - Run the project's own verification commands")
	fmt.Println("  ailoop doctor                   - Show which external tools this machine offers")
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

// runDashboard is what "ailoop" with no arguments shows: where the work is,
// and what can be done next, instead of a wall of usage text.
func runDashboard(cwd string) int {
	pterm.DefaultHeader.WithFullWidth().Println("AI Loop CLI")

	for {
		status := "not initialized - run 'ailoop start'"
		if s, err := state.Load(cwd); err == nil {
			status = fmt.Sprintf("%s (v%d)", s.CurrentPhase, s.CurrentVersion())
			if _, total := s.Spend.Total(); total.Total() > 0 {
				mark := ""
				if total.Estimated {
					mark = " est."
				}
				status += fmt.Sprintf("  -  %d in / %d out tokens%s",
					total.InputTokens, total.OutputTokens, mark)
			}
		}
		pterm.Info.Printf("Current state: %s\n\n", status)

		const (
			optNext   = "Advance (next)"
			optWatch  = "Watch (autonomous TDD)"
			optStatus = "Status"
			optCost   = "Cost"
			optDecs   = "Decisions"
			optUndo   = "Undo"
			optExit   = "Exit"
		)
		selected, _ := pterm.DefaultInteractiveSelect.
			WithOptions([]string{optNext, optWatch, optStatus, optCost, optDecs, optUndo, optExit}).
			Show("Select action")
		fmt.Println()

		// Each option runs the command through run(), not through main(): a
		// dashboard that called main() would recurse, and any os.Exit inside
		// would skip the deferred cleanup of the MCP servers.
		switch selected {
		case optNext:
			return run([]string{"ailoop", "next"})
		case optWatch:
			return run([]string{"ailoop", "watch"})
		case optExit:
			return 0
		case optStatus:
			run([]string{"ailoop", "status"})
		case optCost:
			run([]string{"ailoop", "cost"})
		case optDecs:
			run([]string{"ailoop", "decisions"})
		case optUndo:
			run([]string{"ailoop", "undo"})
		}
		// The read-only actions come back to the menu instead of exiting,
		// which is what you want when you are looking around.
		fmt.Println()
	}
}
