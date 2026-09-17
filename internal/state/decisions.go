package state

import (
	"fmt"
	"strings"
	"time"
)

// Decision is one recorded choice. The Decision Record is the authoritative
// source of what was decided: later stages may not silently contradict it
// (AI_LOOP 5 and 18.2).
type Decision struct {
	ID        string    `json:"id"`
	Statement string    `json:"statement"`
	Rationale string    `json:"rationale,omitempty"`
	Evidence  []string  `json:"evidence,omitempty"`
	Phase     Phase     `json:"phase"`
	Version   int       `json:"version"`
	At        time.Time `json:"at"`
	// SupersededBy is the ID of the decision that replaced this one. A
	// superseded decision is kept, never deleted: the history is the point.
	SupersededBy string `json:"superseded_by,omitempty"`
}

func (d Decision) Active() bool { return d.SupersededBy == "" }

// DecisionRecord holds every decision, active and superseded.
type DecisionRecord struct {
	Decisions []Decision `json:"decisions"`
}

// Add records a new decision and returns its ID.
func (r *DecisionRecord) Add(statement, rationale string, evidence []string, phase Phase) Decision {
	d := Decision{
		ID:        fmt.Sprintf("D-%03d", len(r.Decisions)+1),
		Statement: statement,
		Rationale: rationale,
		Evidence:  evidence,
		Phase:     phase,
		Version:   1,
		At:        time.Now(),
	}
	r.Decisions = append(r.Decisions, d)
	return d
}

// Supersede replaces an existing decision with a new one, keeping both.
func (r *DecisionRecord) Supersede(oldID, statement, rationale string, evidence []string, phase Phase) (Decision, error) {
	idx := -1
	for i := range r.Decisions {
		if r.Decisions[i].ID == oldID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Decision{}, fmt.Errorf("no decision with id %q", oldID)
	}
	if !r.Decisions[idx].Active() {
		return Decision{}, fmt.Errorf("%s is already superseded by %s", oldID, r.Decisions[idx].SupersededBy)
	}

	newer := r.Add(statement, rationale, evidence, phase)
	newer.Version = r.Decisions[idx].Version + 1
	r.Decisions[len(r.Decisions)-1] = newer
	r.Decisions[idx].SupersededBy = newer.ID

	return newer, nil
}

// Active returns only the decisions still in force.
func (r *DecisionRecord) Active() []Decision {
	var out []Decision
	for _, d := range r.Decisions {
		if d.Active() {
			out = append(out, d)
		}
	}
	return out
}

// Brief renders the active decisions for injection into an agent's context.
// This is what stops a later agent from silently contradicting an earlier
// approved decision: it cannot claim it did not know.
func (r *DecisionRecord) Brief() string {
	active := r.Active()
	if len(active) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<APPROVED_DECISIONS>\n")
	b.WriteString("These decisions are authoritative and were approved by the user.\n")
	b.WriteString("You MUST NOT contradict them. If evidence invalidates one, say so explicitly instead of ignoring it.\n\n")
	for _, d := range active {
		fmt.Fprintf(&b, "%s (v%d, decided at %s): %s\n", d.ID, d.Version, d.Phase, d.Statement)
		if d.Rationale != "" {
			fmt.Fprintf(&b, "    rationale: %s\n", d.Rationale)
		}
		for _, e := range d.Evidence {
			fmt.Fprintf(&b, "    evidence: %s\n", e)
		}
	}
	b.WriteString("</APPROVED_DECISIONS>\n")
	return b.String()
}
