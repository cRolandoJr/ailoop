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
	AutoRetries     int      `json:"auto_retries,omitempty"`
	// Record is the authoritative source of decisions taken during this work.
	Record DecisionRecord `json:"decision_record"`
	// Spend is what the work has cost so far, per phase.
	Spend Ledger `json:"spend"`
	// FilesRead are the workspace files the agent has actually read during
	// this work item. A patch may only touch a file that appears here: an
	// agent patching a file it never opened has invented the search text.
	FilesRead []string `json:"files_read,omitempty"`
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

// CurrentVersion is the version of the artifact the current phase produces.
//
// This lived duplicated in main.go and in the agents package, because there
// was no obvious place for it. It belongs to the state: the state is what
// knows which artifact each phase writes.
func (s *AIState) CurrentVersion() int {
	if d := s.CurrentDocument(); d != nil {
		return d.Version
	}
	return 1
}

// CurrentDocument is the artifact the current phase produces, or nil when the
// phase produces none.
func (s *AIState) CurrentDocument() *Document {
	switch s.CurrentPhase {
	case PhaseDiscovery:
		return &s.Spec
	case PhaseDesign:
		return &s.Design
	case PhasePlan:
		return &s.Plan
	case PhaseImplementation, PhaseVerification:
		return &s.Decisions
	}
	return nil
}

// Artifacts lists the named artifacts of the loop, in phase order.
func (s *AIState) Artifacts() []NamedDocument {
	return []NamedDocument{
		{"Spec", &s.Spec},
		{"Design", &s.Design},
		{"Plan", &s.Plan},
	}
}

// NamedDocument pairs an artifact with the name people call it by.
type NamedDocument struct {
	Name string
	Doc  *Document
}

// Phases is the loop's sequence, in order. It exists so callers stop writing
// the list out by hand: a phase added here shows up everywhere.
func Phases() []Phase {
	return []Phase{
		PhaseDiscovery, PhaseDesign, PhasePlan,
		PhaseImplementation, PhaseVerification,
	}
}

// NoteFileRead records that the agent read a file. Kept across phases: what
// was read in Discovery is still read when Implementation writes the patch.
func (s *AIState) NoteFileRead(path string) {
	for _, p := range s.FilesRead {
		if p == path {
			return
		}
	}
	s.FilesRead = append(s.FilesRead, path)
}
