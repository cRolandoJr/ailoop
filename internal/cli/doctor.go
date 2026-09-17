package cli

import (
	"fmt"

	"github.com/cRolandoJr/ailoop/internal/host"
	"github.com/pterm/pterm"
)

// Doctor reports what this machine offers and what is lost without it.
//
// Reporting the loss rather than the feature is deliberate: "no @screen" tells
// you what you cannot do, which is the thing you need to decide whether to
// install anything.
func Doctor(r host.Report) {
	pterm.DefaultHeader.WithFullWidth().Println("Host capabilities")
	fmt.Println()

	for _, t := range r.Tools {
		if t.Available() {
			pterm.Success.Printf("%-12s %s\n", t.Name, t.Purpose)
			continue
		}
		line := fmt.Sprintf("%-12s missing (%s) - %s", t.Name, t.Need, t.Enables)
		switch t.Need {
		case host.Required:
			pterm.Error.Println(line)
		case host.Degraded:
			pterm.Warning.Println(line)
		default:
			pterm.Info.Println(line)
		}
	}

	missing := r.Missing()
	if len(missing) == 0 {
		fmt.Println()
		pterm.Success.Println("Everything the loop can use is here.")
		return
	}

	fmt.Println()
	if hint := host.InstallHint(missing); hint != "" {
		fmt.Println(hint)
	}
	if req := r.MissingRequired(); len(req) > 0 {
		fmt.Println()
		pterm.Error.Println("Something required is missing: the loop cannot verify anything.")
	}
}
