package app

import (
	"time"

	"github.com/cRolandoJr/ailoop/internal/patch"
	"github.com/cRolandoJr/ailoop/internal/state"
)

// UndoOutcome is what going back actually did.
type UndoOutcome struct {
	From state.Phase
	To   state.Phase
	// AtFirstPhase is true when there was nowhere to go back to.
	AtFirstPhase bool
	// Rollback is what the file rollback restored and removed, when one ran.
	Rollback *patch.RestoreResult
}

// Undo returns the work to the previous phase, reopening its artifact and
// rolling back the files if the phase we came from wrote any.
func (l *Loop) Undo() (*UndoOutcome, error) {
	s, err := l.load()
	if err != nil {
		return nil, err
	}

	prev := state.PrevPhase(s.CurrentPhase)
	if prev == s.CurrentPhase {
		return &UndoOutcome{From: s.CurrentPhase, To: s.CurrentPhase, AtFirstPhase: true}, nil
	}

	out := &UndoOutcome{From: s.CurrentPhase, To: prev}
	s.CurrentPhase = prev
	s.ReopenCurrentPhase(time.Now())

	// Only the implementation phase writes files, so only going back to it
	// has anything to roll back.
	if prev == state.PhaseImplementation {
		res, rerr := patch.Restore(l.workspace)
		out.Rollback = res
		if rerr != nil {
			_ = l.save(s)
			return out, rerr
		}
	}

	return out, l.save(s)
}
