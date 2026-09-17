package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/agents"
	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/patch"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
	"github.com/cRolandoJr/ailoop/internal/ui"
	"github.com/cRolandoJr/ailoop/internal/verify"
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
		filesFlag := nextCmd.String("files", "", "Comma-separated list of files to provide as project context")
		criticFlag := nextCmd.Bool("critic", false, "Enable adversarial critic engine for this phase")
		inlineFlag := nextCmd.Bool("inline", false, "Paste --files contents into the prompt instead of letting the agent read them")

		// Parse flags after the "next" command
		if err := nextCmd.Parse(os.Args[2:]); err != nil {
			fmt.Printf("Error parsing flags: %v\n", err)
			os.Exit(1)
		}

		s, err := state.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if s.CurrentPhase == state.PhaseDone {
			pterm.Success.Println("This AI Loop is already DONE.")
			os.Exit(0)
		}

		// --files used to paste every file in full into every request of every
		// round, whether the agent needed it or not. Now that it can read and
		// search the workspace itself, the default is to point at the files
		// and let it fetch what it actually uses. --inline keeps the old
		// behaviour for a model with no tool budget to spare.
		var projectContext string
		if *filesFlag != "" {
			filePaths := strings.Split(*filesFlag, ",")
			var named []string
			for _, fp := range filePaths {
				fp = strings.TrimSpace(fp)
				if fp == "" {
					continue
				}
				if _, err := os.Stat(fp); err != nil {
					pterm.Warning.Printf("could not find %s: %v\n", fp, err)
					continue
				}
				named = append(named, fp)
			}

			if *inlineFlag {
				for _, fp := range named {
					content, err := os.ReadFile(fp)
					if err != nil {
						pterm.Warning.Printf("could not read %s: %v\n", fp, err)
						continue
					}
					projectContext += fmt.Sprintf("\n--- FILE: %s ---\n%s\n", fp, string(content))
				}
			} else if len(named) > 0 {
				projectContext = "Files the user flagged as relevant:\n  " +
					strings.Join(named, "\n  ") +
					"\nRead the ones you actually need with fs.read; do not assume their contents."
				pterm.Info.Printf("%d file(s) named for the agent (use --inline to paste them in full)\n", len(named))
			}
		}

		client := getLLMClient()
		ctx := context.Background()

		// The agent may only run commands this project declared for itself.
		declaredCmds := map[string]string{}
		var mcpServers []mcp.Server
		if cfg, err := config.Load(cwd); err == nil {
			for _, chk := range cfg.Verify {
				declaredCmds[chk.Name] = chk.Cmd
			}
			mcpServers = cfg.MCP
		}

		// External tool servers, if any. A server that fails to start is
		// reported and the loop continues with fewer capabilities: losing a
		// tool server should not stop the work, but hiding that it is missing
		// would have the agent plan around a tool that is not there.
		var mcpPool *mcp.Pool
		if len(mcpServers) > 0 {
			mcpPool = mcp.Open(ctx, mcpServers)
			defer mcpPool.Close()
			for name, err := range mcpPool.Failed() {
				pterm.Warning.Printf("MCP server %q unavailable: %v\n", name, err)
			}
			if n := mcpPool.Count(); n > 0 {
				pterm.Info.Printf("%d external tools available via MCP\n", n)
			}
		}

		observe := func(res tools.Result) {
			if res.Allowed {
				pterm.Info.Printf("  agent inspected %s: %s\n", res.Request.Cap, res.Request.Arg)
			} else {
				pterm.Warning.Printf("  agent asked for %s: %s -> refused\n", res.Request.Cap, res.Request.Arg)
			}
		}

		spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Running %s Agent...", s.CurrentPhase))

		var proposal string
		var agentErr error

		if *criticFlag && (s.CurrentPhase == state.PhaseDesign || s.CurrentPhase == state.PhaseDiscovery) {
			// CRITIC LOOP
			maxIterations := 3
			for i := 1; i <= maxIterations; i++ {
				proposal, agentErr = agents.RunPhase(ctx, s, projectContext, client, cwd, declaredCmds, mcpPool, observe)
				if agentErr != nil {
					break
				}

				spinner.UpdateText(fmt.Sprintf("Critic Agent analyzing proposal (Iteration %d/%d)...", i, maxIterations))
				criticResp, err := agents.EvaluateProposal(ctx, s.TaskDescription, proposal, client)
				if criticResp != nil {
					s.Spend.Record(s.CurrentPhase, criticResp.Usage)
				}
				if err != nil {
					pterm.Warning.Printf("Critic error: %v\n", err)
					break // Proceed with original proposal if critic crashes
				}

				if criticResp.Pass {
					pterm.Success.Println("Critic approved the proposal!")
					break
				} else {
					spinner.Fail("Critic rejected the proposal.")
					pterm.Error.Printf("Feedback: %s\n", criticResp.Feedback)
					s.RejectionReason = criticResp.Feedback // Feed this back into RunPhase on next loop
					if i == maxIterations {
						pterm.Warning.Println("Max critic iterations reached. Presenting best effort to user.")
					} else {
						spinner, _ = pterm.DefaultSpinner.Start("Agent is revising based on critique...")
					}
				}
			}
			s.RejectionReason = "" // Clear after critic loop
		} else {
			// Normal flow
			proposal, agentErr = agents.RunPhase(ctx, s, projectContext, client, cwd, declaredCmds, mcpPool, observe)
		}

		if agentErr != nil {
			spinner.Fail("Agent encountered an error")
			pterm.Error.Printf("Agent error: %v\n", agentErr)
			os.Exit(1)
		}

		spinner.Success(fmt.Sprintf("%s Agent finished.", s.CurrentPhase))

		// Use panel to display proposal
		panel := pterm.DefaultBox.WithTitle(fmt.Sprintf("PHASE: %s (v%d)", s.CurrentPhase, getCurrentVersion(s))).Sprint(proposal)
		fmt.Println(panel)

		approved, rejectionReason := ui.AskApprovalWithReason()

		if !approved {
			s.RejectionReason = rejectionReason
			rejectProposal(s, proposal, rejectionReason)
			if err := state.Save(cwd, s); err != nil {
				pterm.Error.Printf("Failed to save state: %v\n", err)
				os.Exit(1)
			}
			pterm.Info.Printf("State updated to v%d. Run 'ailoop next' to retry with your feedback.\n", getCurrentVersion(s))
			os.Exit(0)
		}

		// Clear rejection reason on success
		s.RejectionReason = ""

		// Save the approved output into the state
		switch s.CurrentPhase {
		case state.PhaseDiscovery:
			s.Spec.Content = proposal
			s.Spec.Approve()
		case state.PhaseDesign:
			s.Design.Content = proposal
			s.Design.Approve()
		case state.PhasePlan:
			s.Plan.Content = proposal
			s.Plan.Approve()
		case state.PhaseImplementation:
			if err := patch.Apply(cwd, proposal); err != nil {
				pterm.Warning.Printf("Implementation approved, but failed to auto-apply patch: %v\n", err)
				pterm.Info.Println("Please manually apply the code changes shown above.")
				s.Decisions.Content += "\n[Implementation manually applied]"
			} else {
				pterm.Success.Println("Code patches successfully applied to files!")
				s.Decisions.Content += "\n[Implementation auto-applied]"
			}
		case state.PhaseVerification:
			s.Decisions.Content += "\n[Verification complete]"
		}

		// Advance state. DONE is not granted by the agent or by the user
		// clicking approve: it is granted by the project's own commands.
		nextPhase := state.NextPhase(s.CurrentPhase)

		// Guard first: the engine decides whether the state may move, not the
		// agent and not the fact that a turn finished.
		if err := state.CanTransition(s, nextPhase); err != nil {
			pterm.Error.Printf("%v\n", err)
			for _, m := range state.MissingFor(s, nextPhase) {
				pterm.Error.Printf("  missing: %s\n", m)
			}
			if err := state.Save(cwd, s); err != nil {
				pterm.Error.Printf("Failed to save state: %v\n", err)
			}
			os.Exit(1)
		}

		if nextPhase == state.PhaseDone && !gateToDone(cwd) {
			if err := state.Save(cwd, s); err != nil {
				pterm.Error.Printf("Failed to save state: %v\n", err)
			}
			os.Exit(1)
		}

		s.CurrentPhase = nextPhase
		if err := state.Save(cwd, s); err != nil {
			pterm.Error.Printf("Failed to save state: %v\n", err)
			os.Exit(1)
		}

		pterm.Success.Printf("Phase completed! Next up: %s\n", s.CurrentPhase)
		if _, total := s.Spend.Total(); total.Total() > 0 {
			mark := ""
			if total.Estimated {
				mark = " (estimated)"
			}
			pterm.Info.Printf("Spend so far: %d in / %d out, %.0f%% from cache%s\n",
				total.InputTokens, total.OutputTokens, 100*total.CacheHitRate(), mark)
		}

	case "status":
		s, err := state.Load(cwd)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Current AI Loop status:\n")
		fmt.Printf("  Task:  %s\n", s.TaskDescription)
		fmt.Printf("  Phase: %s (v%d)\n", s.CurrentPhase, getCurrentVersion(s))
		for _, a := range []struct {
			name string
			doc  state.Document
		}{
			{"Spec", s.Spec}, {"Design", s.Design}, {"Plan", s.Plan},
		} {
			mark := "pending"
			if a.doc.Approved {
				mark = "approved"
			} else if a.doc.Content != "" {
				mark = "produced, NOT approved"
			}
			fmt.Printf("    %-8s v%d  %s\n", a.name, a.doc.Version, mark)
		}

	case "undo":
		s, err := state.Load(cwd)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		prev := state.PrevPhase(s.CurrentPhase)
		if prev == s.CurrentPhase {
			fmt.Println("Cannot undo: already at the very first phase (DISCOVERY).")
			os.Exit(0)
		}

		s.CurrentPhase = prev
		// Going back reopens the artifact of that phase: its approval belonged
		// to a decision we are now revisiting, so it no longer holds. The
		// content stays, archived and recoverable.
		reopenPhase(s)

		if err := state.Save(cwd, s); err != nil {
			pterm.Error.Printf("Failed to save state: %v\n", err)
			os.Exit(1)
		}

		pterm.Info.Printf("⏪ State reverted to %s (v%d).\n", s.CurrentPhase, getCurrentVersion(s))

		// Attempt to restore backups if we are rolling back from Verification (meaning Implementation happened)
		if s.CurrentPhase == state.PhaseImplementation {
			pterm.Info.Println("Rolling back file changes...")
			res, err := patch.Restore(cwd)
			if err != nil {
				pterm.Error.Printf("Rollback incomplete: %v\n", err)
			}
			for _, f := range res.Restored {
				pterm.Success.Printf("  restored %s\n", f)
			}
			for _, f := range res.Deleted {
				pterm.Success.Printf("  removed  %s (created by this loop)\n", f)
			}
			for f, e := range res.Failed {
				pterm.Error.Printf("  FAILED   %s: %v\n", f, e)
			}
			if len(res.Restored) == 0 && len(res.Deleted) == 0 && len(res.Failed) == 0 {
				pterm.Info.Println("  nothing to roll back")
			}
		}

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
		s, err := state.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(s.Spend.Report())

	case "capabilities":
		client := getLLMClient()
		caps := client.Describe()
		fmt.Printf("Provider: %s\n", caps.Provider)
		fmt.Printf("Model:    %s\n", caps.Model)
		fmt.Printf("Vision:   %s\n", caps.Vision)
		fmt.Printf("Thinking: %s\n", caps.Thinking)
		if caps.MaxContextTokens > 0 {
			fmt.Printf("Context:  %d tokens\n", caps.MaxContextTokens)
		} else {
			fmt.Printf("Context:  unknown\n")
		}
		fmt.Println()
		fmt.Println("Capabilities offered to agents, by phase:")
		for _, ph := range []state.Phase{
			state.PhaseDiscovery, state.PhaseDesign, state.PhasePlan,
			state.PhaseImplementation, state.PhaseVerification,
		} {
			reg := agents.RegistryFor(ph, nil, caps, nil)
			var granted []string
			for _, c := range tools.All {
				if reg.Allowed[c] {
					granted = append(granted, string(c))
				}
			}
			fmt.Printf("  %-16s %s\n", ph, strings.Join(granted, ", "))
		}

	case "decide":
		if len(os.Args) < 3 {
			fmt.Println("Usage: ailoop decide \"statement\" [rationale]")
			os.Exit(1)
		}
		s, err := state.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		rationale := ""
		if len(os.Args) > 3 {
			rationale = os.Args[3]
		}
		d := s.Record.Add(os.Args[2], rationale, nil, s.CurrentPhase)
		if err := state.Save(cwd, s); err != nil {
			pterm.Error.Printf("Failed to save state: %v\n", err)
			os.Exit(1)
		}
		pterm.Success.Printf("%s recorded: %s\n", d.ID, d.Statement)

	case "decisions":
		s, err := state.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		if len(s.Record.Decisions) == 0 {
			pterm.Info.Println("No decisions recorded yet.")
			break
		}
		for _, d := range s.Record.Decisions {
			mark := "active"
			if !d.Active() {
				mark = "superseded by " + d.SupersededBy
			}
			fmt.Printf("%s v%d [%s] %s (%s)\n", d.ID, d.Version, mark, d.Statement, d.Phase)
			if d.Rationale != "" {
				fmt.Printf("      why: %s\n", d.Rationale)
			}
		}

	case "history":
		s, err := state.Load(cwd)
		if err != nil {
			pterm.Error.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		for _, a := range []struct {
			name string
			doc  state.Document
		}{{"Spec", s.Spec}, {"Design", s.Design}, {"Plan", s.Plan}} {
			fmt.Printf("%s: v%d current, %d archived revision(s)\n", a.name, a.doc.Version, len(a.doc.History))
			for _, r := range a.doc.History {
				reason := r.RejectedBecause
				if reason == "" {
					reason = "(no reason recorded)"
				}
				fmt.Printf("    v%d  %s  rejected: %s\n", r.Version, r.At.Format("2006-01-02 15:04"), reason)
			}
		}

	case "verify":
		results, ok := runVerification(cwd)
		printVerification(results)
		if !ok {
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func getCurrentVersion(s *state.AIState) int {
	switch s.CurrentPhase {
	case state.PhaseDiscovery:
		return s.Spec.Version
	case state.PhaseDesign:
		return s.Design.Version
	case state.PhaseImplementation, state.PhaseVerification:
		return s.Decisions.Version
	}
	return 1
}

// rejectProposal archives the turned-down proposal with its reason, so that
// going around the loop again does not erase what was already written.
func rejectProposal(s *state.AIState, proposal, reason string) {
	now := time.Now()
	switch s.CurrentPhase {
	case state.PhaseDiscovery:
		s.Spec.RejectProposal(proposal, reason, now)
	case state.PhaseDesign:
		s.Design.RejectProposal(proposal, reason, now)
	case state.PhasePlan:
		s.Plan.RejectProposal(proposal, reason, now)
	case state.PhaseImplementation, state.PhaseVerification:
		s.Decisions.RejectProposal(proposal, reason, now)
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

// runVerification executes the project's declared checks and reports whether
// every one of them passed.
func runVerification(cwd string) ([]verify.Result, bool) {
	cfg, err := config.Load(cwd)
	if err != nil {
		if err == config.ErrNotFound {
			pterm.Error.Printf("No verification config. Expected %s\n", config.Path(cwd))
			pterm.Info.Println("Run 'ailoop start' to create one, or write it by hand.")
		} else {
			pterm.Error.Printf("Could not read verification config: %v\n", err)
		}
		return nil, false
	}

	if len(cfg.Verify) == 0 {
		// An empty check list is not a pass. Nothing ran.
		pterm.Error.Printf("No verification commands declared in %s\n", config.Path(cwd))
		return nil, false
	}

	spinner, _ := pterm.DefaultSpinner.Start("Running project verification...")
	results := verify.Run(
		context.Background(),
		cwd,
		cfg.Verify,
		time.Duration(cfg.TimeoutSeconds)*time.Second,
	)
	spinner.Stop()

	return results, verify.AllPassed(results)
}

func printVerification(results []verify.Result) {
	for _, r := range results {
		label := fmt.Sprintf("%s  (%s)  %s", r.Name, r.Cmd, r.Duration.Round(time.Millisecond))
		if r.Passed {
			pterm.Success.Println(label)
			continue
		}
		if r.Err != nil {
			pterm.Error.Printf("%s - could not run: %v\n", label, r.Err)
		} else {
			pterm.Error.Printf("%s - exit %d\n", label, r.ExitCode)
		}
		if r.Output != "" {
			fmt.Println(pterm.DefaultBox.WithTitle("output: " + r.Name).Sprint(r.Output))
		}
	}
}

// gateToDone is the mechanical half of the protocol: the workflow, not the
// agent, decides whether the work may close.
func gateToDone(cwd string) bool {
	pterm.DefaultHeader.WithFullWidth().Println("CLOSURE GATE")
	pterm.Info.Println("DONE is granted by the project's checks, not by the agent.")

	results, ok := runVerification(cwd)
	printVerification(results)

	if !ok {
		pterm.Error.Println("Verification did not pass. The loop stays in VERIFICATION.")
		pterm.Info.Println("Fix the failures (or 'ailoop undo') and run 'ailoop next' again.")
		return false
	}

	pterm.Success.Println("All checks passed. Transition to DONE authorized.")
	return true
}

// reopenPhase withdraws the approval of the artifact we just returned to and
// archives its current content. The approval was granted for a state of the
// work that no longer holds; keeping it would let a guard wave through
// something nobody re-approved.
func reopenPhase(s *state.AIState) {
	now := time.Now()
	var doc *state.Document
	switch s.CurrentPhase {
	case state.PhaseDiscovery:
		doc = &s.Spec
	case state.PhaseDesign:
		doc = &s.Design
	case state.PhasePlan:
		doc = &s.Plan
	case state.PhaseImplementation, state.PhaseVerification:
		doc = &s.Decisions
	default:
		return
	}
	if doc.Content != "" {
		doc.RejectProposal(doc.Content, "reopened by undo", now)
		doc.Content = ""
	}
	doc.Approved = false
}

// geminiModel lets the model be configured instead of frozen in the binary.
func geminiModel() string {
	if m := os.Getenv("GEMINI_MODEL"); m != "" {
		return m
	}
	return "gemini-2.5-pro"
}
