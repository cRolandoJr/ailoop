package app

import (
	"time"

	"github.com/cRolandoJr/ailoop/internal/agents"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// ArtifactState is where one artifact of the loop stands.
type ArtifactState struct {
	Name     string
	Version  int
	Approved bool
	// Produced is true when there is content, approved or not. The gap
	// between Produced and Approved is the one that matters: an agent can
	// produce text, only a person can approve it.
	Produced bool
}

// StatusReport answers "where is this work item".
type StatusReport struct {
	Task      string
	Phase     state.Phase
	Version   int
	Artifacts []ArtifactState
}

func (l *Loop) Status() (*StatusReport, error) {
	s, err := l.load()
	if err != nil {
		return nil, err
	}

	r := &StatusReport{
		Task:    s.TaskDescription,
		Phase:   s.CurrentPhase,
		Version: s.CurrentVersion(),
	}
	for _, a := range s.Artifacts() {
		r.Artifacts = append(r.Artifacts, ArtifactState{
			Name:     a.Name,
			Version:  a.Doc.Version,
			Approved: a.Doc.Approved,
			Produced: a.Doc.Content != "",
		})
	}
	return r, nil
}

// Cost returns the ledger and the ceiling it runs against. Formatting is the
// caller's problem.
func (l *Loop) Cost() (*state.Ledger, state.Budget, error) {
	s, err := l.load()
	if err != nil {
		return nil, state.Budget{}, err
	}
	return &s.Spend, s.Budget, nil
}

// RevisionInfo is one archived revision of an artifact.
type RevisionInfo struct {
	Version  int
	At       time.Time
	Rejected string
}

// ArtifactHistory is everything archived for one artifact.
type ArtifactHistory struct {
	Name           string
	CurrentVersion int
	Revisions      []RevisionInfo
}

func (l *Loop) History() ([]ArtifactHistory, error) {
	s, err := l.load()
	if err != nil {
		return nil, err
	}

	var out []ArtifactHistory
	for _, a := range s.Artifacts() {
		h := ArtifactHistory{Name: a.Name, CurrentVersion: a.Doc.Version}
		for _, r := range a.Doc.History {
			h.Revisions = append(h.Revisions, RevisionInfo{
				Version:  r.Version,
				At:       r.At,
				Rejected: r.RejectedBecause,
			})
		}
		out = append(out, h)
	}
	return out, nil
}

func (l *Loop) Decisions() ([]state.Decision, error) {
	s, err := l.load()
	if err != nil {
		return nil, err
	}
	return s.Record.Decisions, nil
}

// Decide records an approved decision and returns it.
func (l *Loop) Decide(statement, rationale string) (state.Decision, error) {
	s, err := l.load()
	if err != nil {
		return state.Decision{}, err
	}
	d := s.Record.Add(statement, rationale, nil, s.CurrentPhase)
	if err := l.save(s); err != nil {
		return state.Decision{}, err
	}
	return d, nil
}

// PhaseGrants is what one phase may use.
type PhaseGrants struct {
	Phase   state.Phase
	Granted []tools.Capability
	// Provider and Model are who actually serves this phase - with routing,
	// phases differ, and a report that named only "the" model would lie.
	Provider string
	Model    string
}

// CapabilitiesReport answers "what can this model do, and what may each phase
// use", which are two different questions: a capability needs both.
type CapabilitiesReport struct {
	Model   llm.Capabilities
	ByPhase []PhaseGrants
}

func (l *Loop) Capabilities() (*CapabilitiesReport, error) {
	if l.client == nil {
		return nil, ErrNoClient
	}

	r := &CapabilitiesReport{Model: l.client.Describe()}

	for _, ph := range state.Phases() {
		// Grants are computed against the client that actually serves the
		// phase: with routing, vision (and so fs.read_image) differs per
		// phase, and grants derived from the default would lie.
		caps := l.clientFor(ph).Describe()
		reg := agents.RegistryFor(ph, l.declaredCommands(), caps, l.pool, nil, l.lspClient)
		g := PhaseGrants{Phase: ph, Provider: caps.Provider, Model: caps.Model}
		for _, c := range tools.All {
			if reg.Allowed[c] {
				g.Granted = append(g.Granted, c)
			}
		}
		r.ByPhase = append(r.ByPhase, g)
	}
	return r, nil
}
