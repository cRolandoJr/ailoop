package cli

import (
	"fmt"

	"github.com/cRolandoJr/ailoop/internal/golden"
	"github.com/pterm/pterm"
)

// GoldenReport renders a golden run.
//
// A failing case prints WHICH check failed, not only that the case did. The
// point of the suite is to say what the circuit stopped doing, and "case red"
// sends you back to run it by hand to find out.
func GoldenReport(r *golden.Run) {
	for _, res := range r.Results {
		label := res.Slug
		if res.Title != "" {
			label += " — " + res.Title
		}
		label = fmt.Sprintf("%s  (%dms)", label, res.LatencyMS)

		switch {
		case res.Err != "":
			// Not a failure of the circuit: a failure to ask it. Kept apart so
			// a broken provider never reads as a broken agent.
			pterm.Warning.Printf("%s - could not run: %s\n", label, res.Err)
		case res.Passed:
			pterm.Success.Println(label)
		default:
			pterm.Error.Println(label)
			for _, c := range res.Checks {
				if !c.Passed {
					fmt.Printf("    x %s\n", c.Describe)
				}
			}
		}
	}

	model := r.Model
	if model == "" {
		model = "unknown model"
	}
	if r.Provider != "" {
		model = r.Provider + "/" + model
	}
	fmt.Printf("\n%d/%d cases passed - %s\n", r.Passed, r.Total, model)
	if r.Note != "" {
		fmt.Printf("note: %s\n", r.Note)
	}
	if !r.AllPassed() {
		pterm.Error.Println("The golden suite is red. A case the circuit no longer catches is a finding about the circuit, not about the case.")
	}
}
