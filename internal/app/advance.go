package app

import (
	"context"
	"fmt"
	"time"

	"github.com/cRolandoJr/ailoop/internal/agents"
	"github.com/cRolandoJr/ailoop/internal/patch"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// MaxAutoFixes bounds the self-repair loop. Without a ceiling, a project whose
// tests cannot pass keeps burning tokens forever.
const MaxAutoFixes = 3

// ReviewRequest is a proposal put in front of the human.
type ReviewRequest struct {
	Phase    state.Phase
	Version  int
	Proposal string
	// Blocks are the patches the proposal contains, if any. Empty for the
	// phases that produce prose.
	Blocks []patch.Block
}

// ReviewDecision is what the human answered.
type ReviewDecision struct {
	Approved bool
	Reason   string
	// ApprovedBlocks is the subset of patches accepted, when the review was
	// done patch by patch.
	ApprovedBlocks []patch.Block
}

// AdvanceOptions configures one turn of the loop.
//
// The callbacks are how the use case talks to the outside without depending on
// it: a terminal passes functions that print and prompt, a test passes
// functions that record and answer. That is the whole reason this is testable.
type AdvanceOptions struct {
	ProjectContext string
	UseCritic      bool

	// Review is mandatory: nothing advances without a human answering.
	Review func(ReviewRequest) ReviewDecision
	// OnTool is called for every workspace inspection the agent makes, so the
	// user sees what it looked at instead of trusting it.
	OnTool func(tools.Result)
	// OnProgress reports stages: "phase-start", "critic", "auto-fix"...
	OnProgress func(stage, detail string)
}

func (o AdvanceOptions) progress(stage, detail string) {
	if o.OnProgress != nil {
		o.OnProgress(stage, detail)
	}
}

// AdvanceOutcome is what happened in this turn.
type AdvanceOutcome struct {
	From state.Phase
	To   state.Phase
	// Advanced is false when the proposal was rejected or a gate refused.
	Advanced bool
	// Rejected is true when the human turned the proposal down.
	Rejected bool
	// Verification is present when the DONE gate ran.
	Verification *VerifyReport
	// AutoFixes is how many self-repair rounds this turn used.
	AutoFixes int
}

// Advance runs one turn: the agent proposes, the human reviews, and the engine
// decides whether the state may move.
//
// The self-repair loop that used to be a goto is the for below. A backwards
// goto is always a loop; writing it as one is what makes the cycle visible.
func (l *Loop) Advance(ctx context.Context, opts AdvanceOptions) (*AdvanceOutcome, error) {
	if l.client == nil {
		return nil, ErrNoClient
	}
	if opts.Review == nil {
		return nil, fmt.Errorf("Advance needs a Review callback: nothing advances without a human")
	}

	s, err := l.load()
	if err != nil {
		return nil, err
	}
	if s.CurrentPhase == state.PhaseDone {
		return &AdvanceOutcome{From: s.CurrentPhase, To: s.CurrentPhase}, nil
	}

	out := &AdvanceOutcome{From: s.CurrentPhase}

	// Every file the agent reads is recorded, because a patch may only touch
	// a file it actually opened. The wrapper keeps the caller's own observer
	// working: the CLI still gets to show what was inspected.
	userObserver := opts.OnTool
	opts.OnTool = func(res tools.Result) {
		if res.Allowed && res.Err == nil && res.Request.Cap == tools.FSRead {
			if rel, err := patch.SafeRelPath(l.workspace, res.Request.Arg); err == nil {
				s.NoteFileRead(rel)
			}
		}
		if userObserver != nil {
			userObserver(res)
		}
	}

	for {
		opts.progress("phase-start", string(s.CurrentPhase))

		proposal, err := l.propose(ctx, s, opts)
		if err != nil {
			return nil, err
		}

		blocks, _ := patch.ParseBlocks(proposal)
		decision := opts.Review(ReviewRequest{
			Phase:    s.CurrentPhase,
			Version:  s.CurrentVersion(),
			Proposal: proposal,
			Blocks:   blocks,
		})

		// Patches the human accepted are applied whether or not the whole
		// proposal was approved: a partial review approves work, not a turn.
		if len(decision.ApprovedBlocks) > 0 {
			if err := patch.ApplyBlocks(l.workspace, decision.ApprovedBlocks, l.seen(s)); err != nil {
				return nil, fmt.Errorf("applying approved patches: %w", err)
			}
		}

		if !decision.Approved {
			s.RejectionReason = decision.Reason
			s.RejectCurrentProposal(proposal, decision.Reason, time.Now())
			out.Rejected = true
			out.To = s.CurrentPhase
			return out, l.save(s)
		}

		s.RejectionReason = ""
		if err := l.recordApproved(s, proposal, len(blocks) > 0, &opts); err != nil {
			return nil, err
		}

		next := state.NextPhase(s.CurrentPhase)

		// The guard answers before anything moves: an agent finishing its turn
		// is not permission to advance.
		if err := state.CanTransition(s, next); err != nil {
			_ = l.save(s)
			return nil, &GuardError{Err: err, Missing: state.MissingFor(s, next)}
		}

		if next == state.PhaseDone {
			report, verr := l.Verify(ctx)
			out.Verification = report
			if verr != nil {
				_ = l.save(s)
				return nil, verr
			}
			if !report.Passed {
				// DONE is granted by the project's checks. If they fail, the
				// loop either repairs itself or stops - it never closes.
				if s.AutoRetries < MaxAutoFixes {
					s.AutoRetries++
					out.AutoFixes = s.AutoRetries
					opts.progress("auto-fix",
						fmt.Sprintf("%d/%d", s.AutoRetries, MaxAutoFixes))

					l.rollbackToImplementation(s, report.FailureSummary())
					if err := l.save(s); err != nil {
						return nil, err
					}
					continue // this is the old goto run_agent
				}
				_ = l.save(s)
				out.To = s.CurrentPhase
				return out, nil
			}
			s.AutoRetries = 0
		}

		s.CurrentPhase = next
		out.To = next
		out.Advanced = true
		return out, l.save(s)
	}
}

// GuardError is a refused transition, with everything that was missing.
type GuardError struct {
	Err     error
	Missing []string
}

func (e *GuardError) Error() string { return e.Err.Error() }
func (e *GuardError) Unwrap() error { return e.Err }

// propose runs the phase agent, with the adversarial critic when asked.
func (l *Loop) propose(ctx context.Context, s *state.AIState, opts AdvanceOptions) (string, error) {
	run := func() (string, error) {
		return agents.RunPhase(ctx, s, opts.ProjectContext, l.client,
			l.workspace, l.declaredCommands(), l.pool, l.web(), opts.OnTool)
	}

	criticApplies := opts.UseCritic &&
		(s.CurrentPhase == state.PhaseDesign || s.CurrentPhase == state.PhaseDiscovery)
	if !criticApplies {
		return run()
	}

	const maxIterations = 3
	var proposal string
	for i := 1; i <= maxIterations; i++ {
		var err error
		proposal, err = run()
		if err != nil {
			return "", err
		}

		critique, err := agents.EvaluateProposal(ctx, s.TaskDescription, proposal, l.client)
		if critique != nil {
			s.Spend.Record(s.CurrentPhase, critique.Usage)
		}
		if err != nil {
			// A critic that crashes must not block the work: the proposal
			// still goes to the human, who is the authority anyway.
			opts.progress("critic-error", err.Error())
			break
		}
		if critique.Pass {
			opts.progress("critic-pass", fmt.Sprintf("%d/%d", i, maxIterations))
			break
		}

		opts.progress("critic-reject", critique.Feedback)
		s.RejectionReason = critique.Feedback
		if i == maxIterations {
			opts.progress("critic-exhausted", "presenting best effort")
		}
	}
	s.RejectionReason = ""
	return proposal, nil
}

// recordApproved stores the approved output for the current phase.
func (l *Loop) recordApproved(s *state.AIState, proposal string, hadBlocks bool, opts *AdvanceOptions) error {
	switch s.CurrentPhase {
	case state.PhaseDiscovery, state.PhaseDesign, state.PhasePlan:
		s.RecordApproval(proposal)
	case state.PhaseImplementation:
		if hadBlocks {
			// Already applied block by block during review.
			s.AppendNote("[Implementation applied from reviewed patches]")
			break
		}
		if err := patch.Apply(l.workspace, proposal, l.seen(s)); err != nil {
			opts.progress("patch-failed", err.Error())
			s.AppendNote("[Implementation manually applied]")
			break
		}
		s.AppendNote("[Implementation auto-applied]")
	case state.PhaseVerification:
		s.AppendNote("[Verification complete]")
	}
	return nil
}

// rollbackToImplementation reopens the work for another attempt, handing the
// agent the failures as the reason.
func (l *Loop) rollbackToImplementation(s *state.AIState, failures string) {
	now := time.Now()
	s.ReopenCurrentPhase(now)
	s.CurrentPhase = state.PhaseImplementation
	s.ReopenCurrentPhase(now)
	_, _ = patch.Restore(l.workspace)
	s.RejectionReason = failures
}

// seen is the set of files the agent read, as patch expects it.
//
// Only what the agent actually opened counts. A file named with --files does
// NOT: the user pointing at a file says it is relevant, not what is inside it,
// and the search block still has to come from somewhere real.
func (l *Loop) seen(s *state.AIState) patch.Seen {
	return patch.NewSeen(s.FilesRead...)
}
