package state

type Phase string

const (
	PhaseDiscovery      Phase = "DISCOVERY"
	PhaseDesign         Phase = "DESIGN"
	PhasePlan           Phase = "PLAN"
	PhaseImplementation Phase = "IMPLEMENTATION"
	PhaseVerification   Phase = "VERIFICATION"
	PhaseDone           Phase = "DONE"
)

type Document struct {
	Version int    `json:"version"`
	Content string `json:"content"`
	// Approved records that a human accepted this artifact. Content alone is
	// not approval: an agent can produce text, only a person can approve it.
	Approved bool `json:"approved"`
	// History keeps every superseded revision. Going back must not destroy
	// what was written (AI_LOOP 18.2, 20.5).
	History []Revision `json:"history,omitempty"`
}

type AIState struct {
	TaskDescription string   `json:"task_description"`
	CurrentPhase    Phase    `json:"current_phase"`
	Spec            Document `json:"spec"`
	Design          Document `json:"design"`
	Plan            Document `json:"plan"`
	TestStrategy    Document `json:"test_strategy"`
	Decisions       Document `json:"decisions"`
	RejectionReason string   `json:"rejection_reason,omitempty"`
	// Record is the authoritative source of decisions taken during this work.
	Record DecisionRecord `json:"decision_record"`
	// Spend is what the work has cost so far, per phase.
	Spend Ledger `json:"spend"`
}

// NextPhase returns the next valid phase in the loop
func NextPhase(current Phase) Phase {
	switch current {
	case PhaseDiscovery:
		return PhaseDesign
	case PhaseDesign:
		return PhasePlan
	case PhasePlan:
		return PhaseImplementation
	case PhaseImplementation:
		return PhaseVerification
	case PhaseVerification:
		return PhaseDone
	default:
		return PhaseDone
	}
}

// PrevPhase returns the previous phase (for Undo)
func PrevPhase(current Phase) Phase {
	switch current {
	case PhaseDiscovery:
		return PhaseDiscovery // Can't go back from first phase
	case PhaseDesign:
		return PhaseDiscovery
	case PhasePlan:
		return PhaseDesign
	case PhaseImplementation:
		return PhasePlan
	case PhaseVerification:
		return PhaseImplementation
	case PhaseDone:
		return PhaseVerification
	default:
		return PhaseDiscovery
	}
}

// NewState creates a new initial state
func NewState(task string) *AIState {
	return &AIState{
		TaskDescription: task,
		CurrentPhase:    PhaseDiscovery,
		Spec:            Document{Version: 1},
		Design:          Document{Version: 1},
		Plan:            Document{Version: 1},
		TestStrategy:    Document{Version: 1},
		Decisions:       Document{Version: 1},
	}
}
