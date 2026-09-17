package state

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

// Ledger is what this work item has cost, broken down by phase.
//
// It exists so that every optimisation can be argued with a measurement
// instead of an opinion. A change that is supposed to save tokens and does not
// show up here did not save tokens.
type Ledger struct {
	Phases map[Phase]*PhaseSpend `json:"phases,omitempty"`
}

// PhaseSpend is the running total for one phase.
type PhaseSpend struct {
	Calls int       `json:"calls"`
	Usage llm.Usage `json:"usage"`
}

// Record adds one call to the ledger.
func (l *Ledger) Record(phase Phase, u llm.Usage) {
	if l.Phases == nil {
		l.Phases = map[Phase]*PhaseSpend{}
	}
	ps, ok := l.Phases[phase]
	if !ok {
		ps = &PhaseSpend{}
		l.Phases[phase] = ps
	}
	ps.Calls++
	ps.Usage.Add(u)
}

// Total sums every phase.
func (l *Ledger) Total() (calls int, u llm.Usage) {
	for _, ps := range l.Phases {
		calls += ps.Calls
		u.Add(ps.Usage)
	}
	return calls, u
}

// Report renders the ledger for a human.
func (l *Ledger) Report() string {
	if len(l.Phases) == 0 {
		return "No model calls recorded yet."
	}

	// Ordered by the loop's own sequence, not alphabetically: the shape of
	// the spend across the phases is the thing worth seeing.
	order := []Phase{PhaseDiscovery, PhaseDesign, PhasePlan, PhaseImplementation, PhaseVerification}
	seen := map[Phase]bool{}
	for _, p := range order {
		seen[p] = true
	}
	var extra []string
	for p := range l.Phases {
		if !seen[p] {
			extra = append(extra, string(p))
		}
	}
	sort.Strings(extra)
	for _, p := range extra {
		order = append(order, Phase(p))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %6s %10s %10s %10s %8s\n", "PHASE", "CALLS", "INPUT", "OUTPUT", "CACHED", "HIT")
	for _, p := range order {
		ps, ok := l.Phases[p]
		if !ok {
			continue
		}
		mark := ""
		if ps.Usage.Estimated {
			mark = " ~"
		}
		fmt.Fprintf(&b, "%-16s %6d %10d %10d %10d %7.0f%%%s\n",
			p, ps.Calls, ps.Usage.InputTokens, ps.Usage.OutputTokens,
			ps.Usage.CacheReadTokens, 100*ps.Usage.CacheHitRate(), mark)
	}

	calls, total := l.Total()
	fmt.Fprintf(&b, "%-16s %6d %10d %10d %10d %7.0f%%\n",
		"TOTAL", calls, total.InputTokens, total.OutputTokens,
		total.CacheReadTokens, 100*total.CacheHitRate())

	if total.Estimated {
		b.WriteString("\n~ some numbers are estimated: the provider did not report usage.\n")
	}
	return b.String()
}
