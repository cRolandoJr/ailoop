package cli

import (
	"fmt"

	"github.com/cRolandoJr/ailoop/internal/app"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
	"github.com/cRolandoJr/ailoop/internal/ui"
	"github.com/pterm/pterm"
)

// Review shows a proposal and asks the human. It is the only place that knows
// the loop is driven from a terminal; the use case just calls a function.
func Review(r app.ReviewRequest) app.ReviewDecision {
	panel := pterm.DefaultBox.
		WithTitle(fmt.Sprintf("PHASE: %s (v%d)", r.Phase, r.Version)).
		Sprint(r.Proposal)
	fmt.Println(panel)

	if len(r.Blocks) > 0 {
		approved, reason, blocks := ui.AskApprovalInteractive(r.Proposal)
		return app.ReviewDecision{Approved: approved, Reason: reason, ApprovedBlocks: blocks}
	}

	approved, reason := ui.AskApprovalWithReason()
	return app.ReviewDecision{Approved: approved, Reason: reason}
}

// ToolUsed reports what the agent looked at. An agent inspecting the workspace
// behind the user's back would trade one opacity for another.
func ToolUsed(res tools.Result) {
	if res.Allowed {
		pterm.Info.Printf("  agent inspected %s: %s\n", res.Request.Cap, res.Request.Arg)
	} else {
		pterm.Warning.Printf("  agent asked for %s: %s -> refused\n", res.Request.Cap, res.Request.Arg)
	}
}

// Progress renders the stages the use case reports.
func Progress(stage, detail string) {
	switch stage {
	case "phase-start":
		pterm.DefaultHeader.WithFullWidth().Printf("STARTING %s PHASE", detail)
		fmt.Println()
	case "critic-pass":
		pterm.Success.Printf("Critic approved the proposal (%s)\n", detail)
	case "critic-reject":
		pterm.Error.Printf("Critic rejected the proposal: %s\n", detail)
	case "critic-error":
		pterm.Warning.Printf("Critic error: %s\n", detail)
	case "critic-exhausted":
		pterm.Warning.Printf("Max critic iterations reached: %s\n", detail)
	case "auto-fix":
		pterm.Warning.Printf("Verification failed. Auto-fix %s: rolling back to IMPLEMENTATION...\n", detail)
	case "patch-failed":
		pterm.Warning.Printf("Approved, but the patch did not apply: %s\n", detail)
		pterm.Info.Println("Apply the changes manually.")
	default:
		pterm.Info.Printf("%s: %s\n", stage, detail)
	}
}

// Outcome renders the end of a turn.
func Outcome(out *app.AdvanceOutcome, ledger *state.Ledger) {
	if out.Verification != nil {
		Verification(out.Verification)
	}

	switch {
	case out.Rejected:
		pterm.Info.Println("Proposal rejected and archived. Run 'ailoop next' to retry with your feedback.")
	case out.Advanced && out.To == state.PhaseDone:
		pterm.Success.Println("All checks passed. The loop is DONE.")
	case out.Advanced:
		pterm.Success.Printf("Phase completed! Next up: %s\n", out.To)
	default:
		pterm.Error.Println("The loop did not advance.")
	}

	if ledger != nil {
		if _, total := ledger.Total(); total.Total() > 0 {
			mark := ""
			if total.Estimated {
				mark = " (estimated)"
			}
			pterm.Info.Printf("Spend so far: %d in / %d out, %.0f%% from cache%s\n",
				total.InputTokens, total.OutputTokens, 100*total.CacheHitRate(), mark)
		}
	}
}

// Verification renders the project's own checks.
func Verification(r *app.VerifyReport) {
	for _, res := range r.Results {
		label := fmt.Sprintf("%s  (%s)  %s", res.Name, res.Cmd, res.Duration.Round(1e6))
		if res.Passed {
			pterm.Success.Println(label)
			continue
		}
		if res.Err != nil {
			pterm.Error.Printf("%s - could not run: %v\n", label, res.Err)
		} else {
			pterm.Error.Printf("%s - exit %d\n", label, res.ExitCode)
		}
		if res.Output != "" {
			fmt.Println(pterm.DefaultBox.WithTitle("output: " + res.Name).Sprint(res.Output))
		}
	}
}

// GuardRefusal explains a transition the engine refused, and everything the
// state was missing - all of it at once, not one per attempt.
func GuardRefusal(e *app.GuardError) {
	pterm.Error.Printf("%v\n", e.Err)
	for _, m := range e.Missing {
		pterm.Error.Printf("  missing: %s\n", m)
	}
}

// ProjectContext reports what the agent was pointed at.
func ProjectContext(pc *app.ProjectContext) {
	for _, m := range pc.Missing {
		pterm.Warning.Printf("could not find %s\n", m)
	}
	if len(pc.Named) == 0 {
		return
	}
	if pc.Inlined {
		pterm.Info.Printf("%d file(s) pasted into the prompt\n", len(pc.Named))
		return
	}
	pterm.Info.Printf("%d file(s) named for the agent (use --inline to paste them in full)\n", len(pc.Named))
}

// UndoOutcome renders a rollback, saying what it actually did rather than
// what it intended to do.
func UndoOutcome(out *app.UndoOutcome) {
	if out.AtFirstPhase {
		pterm.Info.Println("Cannot undo: already at the first phase (DISCOVERY).")
		return
	}
	pterm.Info.Printf("State reverted to %s.\n", out.To)

	if out.Rollback == nil {
		return
	}
	for _, f := range out.Rollback.Restored {
		pterm.Success.Printf("  restored %s\n", f)
	}
	for _, f := range out.Rollback.Deleted {
		pterm.Success.Printf("  removed  %s (created by this loop)\n", f)
	}
	for f, e := range out.Rollback.Failed {
		pterm.Error.Printf("  FAILED   %s: %v\n", f, e)
	}
	if len(out.Rollback.Restored) == 0 && len(out.Rollback.Deleted) == 0 && len(out.Rollback.Failed) == 0 {
		pterm.Info.Println("  nothing to roll back")
	}
}
