package cli

import (
	"fmt"

	"github.com/cRolandoJr/ailoop/internal/patch"
	"github.com/pterm/pterm"
)

// ProblemLines renders what the dry run found, one line per block that would
// be refused. It is separate from printing so the wording can be tested.
func ProblemLines(blocks []patch.Block, problems []patch.Problem) []string {
	lines := make([]string, 0, len(problems))
	for _, p := range problems {
		lines = append(lines, fmt.Sprintf("patch %d/%d  %s: %v",
			p.Index+1, len(blocks), p.File, p.Err))
	}
	return lines
}

// ShowPatchProblems puts the dry run in front of the person before they are
// asked to decide. Nothing was written and nothing was run to learn this.
func ShowPatchProblems(blocks []patch.Block, problems []patch.Problem) {
	lines := ProblemLines(blocks, problems)
	if len(lines) == 0 {
		return
	}
	pterm.Warning.Printfln("%d of %d patches would be refused as proposed:", len(lines), len(blocks))
	for _, l := range lines {
		pterm.Println("  " + l)
	}
}
