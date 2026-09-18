package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/app"
	"github.com/cRolandoJr/ailoop/internal/repl"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/ui"
	"github.com/pterm/pterm"
)

// SessionPrompt draws the prompt line, with where the work stands.
//
// The state goes above the cursor rather than in a header printed once,
// because it changes under the person's feet: a phase advances, tokens are
// spent, and a banner from ten screens ago would be describing something else.
func SessionPrompt(s *state.AIState) {
	if s == nil {
		pterm.Println(pterm.Gray("no work started - describe a task to begin"))
		pterm.Print(pterm.LightCyan("> "))
		return
	}

	parts := []string{fmt.Sprintf("%s (v%d)", s.CurrentPhase, s.CurrentVersion())}
	if _, total := s.Spend.Total(); total.Total() > 0 {
		mark := ""
		if total.Estimated {
			mark = " est."
		}
		parts = append(parts, fmt.Sprintf("%d in / %d out%s",
			total.InputTokens, total.OutputTokens, mark))
	}
	if s.Feedback != "" {
		// Feedback still pending is worth seeing: it is what the agent will
		// read on the next phase, and it is easy to forget having typed it.
		parts = append(parts, "feedback pending")
	}

	pterm.Println(pterm.Gray(strings.Join(parts, "  ·  ")))
	pterm.Print(pterm.LightCyan("> "))
}

// SessionHelp lists what the slash commands are.
func SessionHelp(catalog []repl.Info) {
	width := 0
	for _, c := range catalog {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	pterm.Println()
	pterm.Println(pterm.Bold.Sprint("Commands"))
	for _, c := range catalog {
		pterm.Printf("  /%-*s  %s\n", width, c.Name, pterm.Gray(c.Desc))
	}
	pterm.Println()
	pterm.Println(pterm.Bold.Sprint("Anything else you type goes to the agent"))
	pterm.Println("  with no work started, it is the task")
	pterm.Println("  with work under way, it is feedback for the next phase")
	pterm.Println("  on a proposal, Enter approves and text rejects with that reason")
}

// SessionReview reviews a proposal from inside the session, reading the answer
// from the session's own input.
//
// Approval is the empty line and rejection is whatever you type, which folds
// two questions into one gesture: the old flow asked y/N and then asked
// separately for a reason, so saying "no, because X" cost two turns and the
// reason arrived detached from the decision.
//
// The scanner is the session's, not a new one. Two scanners over the same
// stdin would each hold their own buffer, and whichever read first would eat
// input the other was waiting for.
func SessionReview(in *bufio.Scanner) func(app.ReviewRequest) app.ReviewDecision {
	return func(r app.ReviewRequest) app.ReviewDecision {
		panel := pterm.DefaultBox.
			WithTitle(fmt.Sprintf("PHASE: %s (v%d)", r.Phase, r.Version)).
			Sprint(r.Proposal)
		fmt.Println(panel)

		// Patches still need the picker: approving a proposal and approving
		// each edit it contains are different decisions, and one line cannot
		// carry the second.
		if len(r.Blocks) > 0 {
			ShowPatchProblems(r.Blocks, r.Problems)
			approved, reason, blocks := ui.AskApprovalInteractive(r.Proposal)
			return app.ReviewDecision{Approved: approved, Reason: reason, ApprovedBlocks: blocks}
		}

		pterm.Println(pterm.Gray("Enter approves. Type a reason to reject."))
		pterm.Print(pterm.LightCyan("> "))

		if !in.Scan() {
			// EOF mid-review: treat as no approval. Advancing on a closed
			// input would be the loop approving on the person's behalf.
			fmt.Println()
			return app.ReviewDecision{Approved: false, Reason: "input closed before review"}
		}

		answer := strings.TrimSpace(in.Text())
		if answer == "" {
			return app.ReviewDecision{Approved: true}
		}
		return app.ReviewDecision{Approved: false, Reason: answer}
	}
}
