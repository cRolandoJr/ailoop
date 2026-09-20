package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/app"
	"github.com/cRolandoJr/ailoop/internal/cli"
	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/env"
	"github.com/cRolandoJr/ailoop/internal/host"
	"github.com/cRolandoJr/ailoop/internal/ide"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/lsp"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"syscall"

	"github.com/cRolandoJr/ailoop/internal/repl"
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
// interruptible returns a context cancelled by Ctrl-C.
//
// Intercepting the signal instead of letting it kill the process is what
// makes an abandoned run keep its receipt. Cancellation travels out through
// the same error path a network failure takes, and that path already
// persists the ledger - so nothing here needs to know how state is saved. A
// signal handler that saved on its own would have been a second copy of that
// logic, free to drift from the first.
//
// The returned stop is also armed on the first signal, which restores default
// handling: a run that does not react to cancellation must stay killable from
// the keyboard, so the second Ctrl-C ends the process the usual way.
func interruptible() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

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
		return runSession(cwd)
	}

	command := args[1]

	switch command {
	case "start":
		startCmd := flag.NewFlagSet("start", flag.ExitOnError)
		budgetUSD := startCmd.Float64("budget", 0, "abort the work item above this estimated cost in USD (needs prices in the config)")
		budgetTok := startCmd.Int("max-tokens", 0, "abort the work item above this many tokens")
		force := startCmd.Bool("force", false, "discard the work item already in this directory")
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

		// Starting used to save a fresh state over whatever was there, without
		// a word. An approved spec, a ledger and a history of rejected
		// proposals disappeared on a mistyped command.
		if prev, err := state.Load(cwd); err == nil && prev.HasWork() && !*force {
			pterm.Error.Printfln("There is already work here: %s (%s)", prev.TaskDescription, prev.CurrentPhase)
			pterm.Info.Println("Starting over would discard it. Use --force if that is what you want.")
			return 1
		}

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

		// Fail here, not on the first next. A ceiling that cannot be evaluated
		// is a setting the person has to fix, and finding out one command
		// later means they created a work item they cannot run.
		if s.Budget.MaxUSD > 0 && !s.Budget.Priced() {
			cli.Error(fmt.Errorf(
				"a USD ceiling of %.2f needs prices: add price_in_per_mtok and "+
					"price_out_per_mtok under \"budget\" in %s, or use --max-tokens",
				s.Budget.MaxUSD, config.Path(cwd)))
			return 1
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

		ctx, stopSignals := interruptible()
		defer stopSignals()
		loop, pool, err := buildLoop(ctx, cwd)
		if err != nil {
			cli.Error(err)
			return 1
		}
		// The loop owns the language server it started.
		defer loop.Close()
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
		ctx, stopSignals := interruptible()
		defer stopSignals()
		loop, pool, err := buildLoop(ctx, cwd)
		if err != nil {
			cli.Error(err)
			return 1
		}
		// The loop owns the language server it started.
		defer loop.Close()
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
		// Built with the real external tools attached, not the cheap loop:
		// the whole point of this command is what IS granted here, and a loop
		// assembled without the MCP pool and the language server reports them
		// as absent whether they are or not.
		loop, pool, err := buildLoop(context.Background(), cwd)
		if err != nil {
			cli.Error(err)
			return 1
		}
		// The loop owns the language server it started.
		defer loop.Close()
		if pool != nil {
			defer pool.Close()
		}
		r, err := loop.Capabilities()
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
		// The loop owns the language server it started.
		defer loop.Close()
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
	// Env-var precedence, unchanged: the first credential found wins. The
	// construction of each provider (retry policy included) lives once, in
	// llm.FromName; this only picks WHICH one when no config routes phases.
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		if c, err := llm.FromName("claude"); err == nil {
			return c
		}
	}
	if os.Getenv("GEMINI_API_KEY") != "" {
		if c, err := llm.FromName("gemini"); err == nil {
			return c
		}
	}
	// The OpenAI-compatible default (local Ollama) needs no credential and
	// cannot fail to construct.
	c, _ := llm.FromName("openai")
	return c
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
	return app.NewLoop(workspace, client, nil, cfg, nil)
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

	var lspClient *lsp.Client
	var lspCmd []string
	if len(cfg.LSP) > 0 {
		lspCmd = cfg.LSP
	} else {
		if _, err := os.Stat(filepath.Join(workspace, "go.mod")); err == nil {
			lspCmd = []string{"gopls"}
		}
	}
	if len(lspCmd) > 0 {
		client, err := lsp.Start(ctx, lspCmd[0], lspCmd[1:]...)
		if err == nil {
			if err := client.Initialize(ctx, workspace); err != nil {
				pterm.Warning.Printf("LSP %v did not initialize: %v\n", lspCmd, err)
			}
			lspClient = client
		} else {
			pterm.Warning.Printf("Could not start LSP %v: %v\n", lspCmd, err)
		}
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

	// Phase routing (SPEC-ruteo-proveedor-por-fase): a provider NAMED in the
	// config that cannot be built is an error naming its missing credential,
	// never a silent fallback to some other model.
	byPhase, defName := cfg.PhaseProviders()
	def := getLLMClient()
	if defName != "" {
		c, err := llm.FromName(defName)
		if err != nil {
			return nil, nil, fmt.Errorf("providers.default: %w", err)
		}
		def = c
	}
	loop := app.NewLoop(workspace, def, pool, cfg, lspClient)
	if len(byPhase) > 0 {
		routed := map[state.Phase]llm.Client{}
		for ph, name := range byPhase {
			c, err := llm.FromName(name)
			if err != nil {
				return nil, nil, fmt.Errorf("providers.%s: %w", strings.ToLower(string(ph)), err)
			}
			routed[ph] = c
		}
		loop.RouteClients(routed)
	}
	return loop, pool, nil
}

// runSession is what "ailoop" with no arguments opens: one process, one MCP
// pool, and a prompt that takes plain text.
//
// It replaced a menu of fixed options. A menu can only offer what it lists,
// so the thing a person most often wants to say - "no, because X" - had
// nowhere to go until a rejection prompt happened to appear. Typing is the
// general case; the menu was the special one.
//
// State is deliberately NOT held in this loop. It is re-read from disk each
// turn, exactly as the one-shot commands do, so the session is a way to reach
// the work rather than the place the work lives: close the terminal and
// nothing is lost.
func runSession(cwd string) int {
	ctx, stopSignals := interruptible()
	defer stopSignals()

	loop, pool, err := buildLoop(ctx, cwd)
	if err != nil {
		cli.Error(err)
		return 1
	}
	// The loop owns the language server it started.
	defer loop.Close()
	if pool != nil {
		// Opened once for the whole session. The one-shot commands pay this
		// cost on every invocation; here it is paid once and reused.
		defer pool.Close()
	}
	// Only a process that stays alive can receive editor state. A one-shot
	// command would start a server, do its work and exit before a plugin
	// could reach it; it still READS what a session left on disk.
	if err := ide.StartSyncServer(cwd); err != nil {
		pterm.Warning.Println(err)
	}

	pterm.DefaultHeader.WithFullWidth().Println("AI Loop")
	pterm.Info.Println("Type to talk to the agent. /help for commands, /exit to leave.")
	fmt.Println()

	in := bufio.NewScanner(os.Stdin)
	for {
		s, loadErr := state.Load(cwd)
		cli.SessionPrompt(s)

		if !in.Scan() {
			// EOF: Ctrl-D, or stdin was never a terminal.
			fmt.Println()
			return 0
		}

		switch intent := repl.Interpret(in.Text(), loadErr == nil); intent.Kind {
		case repl.KindExit:
			return 0

		case repl.KindNothing:
			pterm.Info.Println("Describe the work you want to start, or /help.")

		case repl.KindStart:
			run([]string{"ailoop", "start", intent.Text})

		case repl.KindAdvance:
			sessionAdvance(ctx, loop, cwd, intent.Text, in)

		case repl.KindNeedsWork:
			pterm.Info.Printfln("/%s needs a work item. Describe the task and press Enter to begin.", intent.Text)

		case repl.KindCommand:
			runSessionCommand(intent)
		}
		fmt.Println()
	}
}

// runSessionCommand handles a slash command.
func runSessionCommand(intent repl.Intent) {
	if intent.Text == "help" {
		cli.SessionHelp(repl.Catalog())
		return
	}
	cmd, known := repl.Command(intent.Text)
	if !known {
		pterm.Warning.Printf("Unknown command %q. /help lists them.\n", intent.Text)
		return
	}
	argv := []string{"ailoop", cmd}
	if intent.Args != "" {
		argv = append(argv, strings.Fields(intent.Args)...)
	}
	run(argv)
}

// sessionAdvance runs one phase, carrying the person's text as feedback.
//
// The feedback is written to the state before the phase runs because that is
// where the agent reads it, and where @references are expanded. Routing it
// anywhere else would have been a second mechanism for the same thing.
func sessionAdvance(ctx context.Context, loop *app.Loop, cwd, feedback string, in *bufio.Scanner) {
	if feedback != "" {
		s, err := state.Load(cwd)
		if err != nil {
			cli.Error(err)
			return
		}
		s.Feedback = feedback
		if err := state.Save(cwd, s); err != nil {
			cli.Error(err)
			return
		}
	}

	pc, err := loop.BuildProjectContext(nil, false)
	if err != nil {
		cli.Error(err)
		return
	}
	cli.ProjectContext(pc)

	out, err := loop.Advance(ctx, app.AdvanceOptions{
		ProjectContext: pc.Text,
		Review:         cli.SessionReview(in),
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
		return
	}

	ledger, _, _ := loop.Cost()
	cli.Outcome(out, ledger)
}
