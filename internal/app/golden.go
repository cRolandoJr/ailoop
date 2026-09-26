package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/agents"
	"github.com/cRolandoJr/ailoop/internal/attach"
	"github.com/cRolandoJr/ailoop/internal/golden"
	"github.com/cRolandoJr/ailoop/internal/state"
)

// ErrNoCases means the project never wrote a golden case.
//
// It is an error and not an empty pass, for the same reason ErrNoChecks is:
// a suite that measured nothing must be loud, not reassuring.
var ErrNoCases = errors.New("no golden cases in golden/: there is nothing to measure")

// servedPhases are the phases that have an agent behind them.
var servedPhases = map[state.Phase]bool{
	state.PhaseDiscovery:      true,
	state.PhaseDesign:         true,
	state.PhasePlan:           true,
	state.PhaseImplementation: true,
	state.PhaseVerification:   true,
}

// Golden runs the project's golden cases through the real phase agent and
// records the run (DR-005).
//
// Each case is a throwaway work item: its own state, its task, its phase, and
// no artifacts carried in. That isolation is the point — a case that passed
// only because a previous case left a spec behind measures the order of the
// suite, not the circuit.
func (l *Loop) Golden(ctx context.Context, note string) (*golden.Run, error) {
	cases, err := golden.LoadCases(l.workspace)
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, ErrNoCases
	}
	if l.client == nil {
		return nil, errors.New("golden needs a model client: none was configured")
	}

	// A phase nobody serves fails every case with the same message, after
	// spending a call on each. Refuse before spending anything.
	//
	// Keyed by slug rather than by position: Execute skips disabled cases
	// without calling the producer, so a counter would drift and hand a case
	// the phase of another one.
	phases := map[string]state.Phase{}
	for _, c := range cases {
		p := state.Phase(strings.ToUpper(strings.TrimSpace(c.Phase)))
		if !servedPhases[p] {
			return nil, fmt.Errorf("case %q: unknown phase %q", c.Slug, c.Phase)
		}
		phases[c.Slug] = p
	}

	resolver := attach.New(l.workspace)
	run := golden.Execute(ctx, cases, func(ctx context.Context, c golden.Case) (string, error) {
		phase := phases[c.Slug]
		if c.Proposal != "" {
			// The critic answers with a structured verdict; the checks are
			// textual. Rendering it deterministically keeps the check
			// vocabulary at three types instead of growing one per producer.
			critique, err := agents.EvaluateProposal(ctx, c.Task, c.Proposal, l.clientFor(phase))
			if err != nil {
				return "", err
			}
			verdict := "FAIL"
			if critique.Pass {
				verdict = "PASS"
			}
			return "VERDICT: " + verdict + "\n\n" + critique.Feedback, nil
		}
		s := &state.AIState{TaskDescription: c.Task, CurrentPhase: phase}
		return l.propose(ctx, s, AdvanceOptions{}, resolver)
	})

	caps := l.client.Describe()
	run.Provider, run.Model, run.Trigger, run.Note = caps.Provider, caps.Model, "manual", note
	if len(l.phaseClients) > 0 {
		// The run-level model is the default one. Saying so is the difference
		// between a record and a misleading record: with routing on, some
		// cases did not run on it.
		run.Note = strings.TrimSpace(run.Note + " (phase routing active: some cases ran on another model)")
	}

	golden.AppendRun(l.workspace, run)
	return &run, nil
}
