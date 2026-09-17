package state

import "time"

// Revision is one past version of an artifact, kept so that going back does
// not mean losing what was written.
//
// AI_LOOP 18.2: "historical artifact versions remain recoverable".
// Before this, rejecting a proposal overwrote the previous content and the
// only trace left was a version number that counted losses.
type Revision struct {
	Version int       `json:"version"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
	// RejectedBecause is why this revision was superseded. Empty when the
	// revision was archived for another reason.
	RejectedBecause string `json:"rejected_because,omitempty"`
}

// Revise archives the current content and installs a new version.
// reason explains why the previous one was replaced.
func (d *Document) Revise(newContent, reason string, now time.Time) {
	if d.Content != "" {
		d.History = append(d.History, Revision{
			Version:         d.Version,
			Content:         d.Content,
			At:              now,
			RejectedBecause: reason,
		})
	}
	d.Version++
	d.Content = newContent
	// A new revision is never carried over as approved: approval belongs to
	// the exact text that was approved.
	d.Approved = false
}

// Approve marks the current content as accepted by a human.
func (d *Document) Approve() {
	d.Approved = true
}

// RevisionAt returns a past revision by version number.
func (d *Document) RevisionAt(version int) (Revision, bool) {
	for _, r := range d.History {
		if r.Version == version {
			return r, true
		}
	}
	return Revision{}, false
}

// RejectProposal archives a proposal the user turned down, with the reason,
// and bumps the version. Content is left untouched: a rejected proposal never
// becomes the current artifact.
//
// Without this, a rejected proposal vanished entirely and the only record was
// a version counter measuring losses.
func (d *Document) RejectProposal(proposal, reason string, now time.Time) {
	d.History = append(d.History, Revision{
		Version:         d.Version,
		Content:         proposal,
		At:              now,
		RejectedBecause: reason,
	})
	d.Version++
	d.Approved = false
}

// RejectCurrentProposal archives a proposal the user turned down, against
// whichever artifact the current phase produces.
//
// It lived in main.go as a free function with a switch over phases. It
// belongs here: the state is what knows which artifact a phase writes.
func (s *AIState) RejectCurrentProposal(proposal, reason string, now time.Time) {
	if d := s.CurrentDocument(); d != nil {
		d.RejectProposal(proposal, reason, now)
	}
}

// ReopenCurrentPhase withdraws the approval of the current phase's artifact
// and archives its content, because the work is going back to it.
//
// The approval was granted for a state of the work that no longer holds;
// keeping it would let a guard wave through something nobody re-approved.
func (s *AIState) ReopenCurrentPhase(now time.Time) {
	d := s.CurrentDocument()
	if d == nil {
		return
	}
	if d.Content != "" {
		d.RejectProposal(d.Content, "reopened", now)
		d.Content = ""
	}
	d.Approved = false
}

// RecordApproval stores an approved proposal as the current artifact.
func (s *AIState) RecordApproval(proposal string) {
	if d := s.CurrentDocument(); d != nil {
		d.Content = proposal
		d.Approve()
	}
}

// AppendNote adds a line to the decisions log of the current phase.
func (s *AIState) AppendNote(note string) {
	s.Decisions.Content += "\n" + note
}
