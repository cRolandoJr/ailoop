package state

// HasWork reports whether this work item holds anything that replacing it
// would destroy.
//
// It exists so that starting a new one can refuse instead of overwriting.
// `ailoop start` used to save a fresh state over whatever was there, without a
// word: a loop with approved artifacts, a ledger and a history of rejected
// proposals disappeared on a mistyped command.
//
// A brand-new state is not work, so beginning twice in an empty project stays
// as easy as it was.
func (s *AIState) HasWork() bool {
	for _, d := range []Document{s.Spec, s.Design, s.Plan, s.TestStrategy, s.Decisions} {
		if d.Approved || len(d.History) > 0 {
			return true
		}
	}
	if _, total := s.Spend.Total(); total.Total() > 0 {
		return true
	}
	return len(s.Record.Decisions) > 0
}
