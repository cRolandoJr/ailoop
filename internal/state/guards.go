package state

import "fmt"

// A transition guard answers one question: may this work item move to the next
// phase, given what actually exists in the state?
//
// The engine asks this, not the agent. An agent finishing its turn is not
// permission to advance: see AI_LOOP 20.4, "no implicit transitions".

// TransitionError explains exactly why a transition was refused.
type TransitionError struct {
	From   Phase
	To     Phase
	Reason string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("transition %s -> %s refused: %s", e.From, e.To, e.Reason)
}

// requirement is one precondition of a transition.
type requirement struct {
	describe string
	met      func(s *AIState) bool
}

func approved(name string, pick func(*AIState) *Document) requirement {
	return requirement{
		describe: name + " must exist and be approved",
		met: func(s *AIState) bool {
			d := pick(s)
			return d.Content != "" && d.Approved
		},
	}
}

func pickSpec(s *AIState) *Document   { return &s.Spec }
func pickDesign(s *AIState) *Document { return &s.Design }
func pickPlan(s *AIState) *Document   { return &s.Plan }

// guards maps each target phase to everything that must hold before entering it.
var guards = map[Phase][]requirement{
	PhaseDesign: {
		approved("Spec (from Discovery)", pickSpec),
	},
	PhasePlan: {
		approved("Spec (from Discovery)", pickSpec),
		approved("Design", pickDesign),
	},
	PhaseImplementation: {
		approved("Design", pickDesign),
		approved("Plan", pickPlan),
	},
	PhaseVerification: {
		approved("Design", pickDesign),
		approved("Plan", pickPlan),
	},
	// PhaseDone is deliberately absent: its gate is the project's own
	// verification commands, which live outside the state. A guard here would
	// be a second, weaker answer to the same question.
}

// CanTransition reports whether the work item may move to the target phase.
// Returns nil when the transition is allowed.
func CanTransition(s *AIState, to Phase) error {
	if s.CurrentPhase == PhaseDone {
		return &TransitionError{From: s.CurrentPhase, To: to, Reason: "the loop is already DONE"}
	}
	if to != NextPhase(s.CurrentPhase) {
		return &TransitionError{From: s.CurrentPhase, To: to, Reason: "not the next phase in the loop"}
	}

	for _, req := range guards[to] {
		if !req.met(s) {
			return &TransitionError{From: s.CurrentPhase, To: to, Reason: req.describe}
		}
	}
	return nil
}

// MissingFor lists every unmet requirement for the given phase, so the user
// sees all of them at once instead of one per attempt.
func MissingFor(s *AIState, to Phase) []string {
	var missing []string
	for _, req := range guards[to] {
		if !req.met(s) {
			missing = append(missing, req.describe)
		}
	}
	return missing
}
