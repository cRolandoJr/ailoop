// Package host reports which external tools this machine offers.
//
// The loop itself is Go and depends on nothing: the state machine, the guards,
// the ledger, the budget and the patching behave the same everywhere. What
// varies is the environment, and AI_LOOP 18.16 is explicit about how that is
// handled - capabilities are discovered, not assumed.
//
// The point of discovering them up front is that a capability whose tool is
// missing is never offered. Failing when the user finally reaches for it, on
// the other hand, is finding out too late.
package host

import (
	"os/exec"
	"sort"
	"strings"
)

// Need is how much the loop depends on a tool.
type Need int

const (
	// Required: the loop cannot do its job without it.
	Required Need = iota
	// Degraded: the loop works, with less.
	Degraded
	// Optional: it enables a capability that simply will not exist.
	Optional
)

func (n Need) String() string {
	switch n {
	case Required:
		return "required"
	case Degraded:
		return "degrades"
	default:
		return "optional"
	}
}

// Tool is one external program the loop can use.
type Tool struct {
	Name string
	// Purpose says what it is for, in the user's terms.
	Purpose string
	// Enables names what stops working without it. Written as the loss, not
	// as the feature: "no @screen" is more useful than "screenshots".
	Enables string
	Need    Need
	// Path is where it was found. Empty means it is not here.
	Path string
}

func (t Tool) Available() bool { return t.Path != "" }

// known is the full set the loop ever looks for.
var known = []Tool{
	{Name: "sh", Purpose: "run the project's verification commands",
		Enables: "verification cannot run at all", Need: Required},
	{Name: "git", Purpose: "read the branch and uncommitted changes",
		Enables: "state is not isolated per branch, and the agent does not see uncommitted work", Need: Degraded},
	{Name: "pdftotext", Purpose: "read PDFs (poppler-utils)",
		Enables: "no @file.pdf", Need: Optional},
	{Name: "grim", Purpose: "capture the screen (Wayland)",
		Enables: "no @screen", Need: Optional},
	{Name: "slurp", Purpose: "select a region to capture (Wayland)",
		Enables: "no @screen:select", Need: Optional},
	{Name: "wl-paste", Purpose: "read the clipboard (Wayland)",
		Enables: "no @clipboard", Need: Optional},
}

// Report is what this machine offers.
type Report struct {
	Tools []Tool
}

// Discover looks for every known tool. It runs once per invocation: the answer
// does not change while the process lives.
func Discover() Report {
	r := Report{Tools: make([]Tool, 0, len(known))}
	for _, t := range known {
		if p, err := exec.LookPath(t.Name); err == nil {
			t.Path = p
		}
		r.Tools = append(r.Tools, t)
	}
	return r
}

// Has reports whether a tool is present.
func (r Report) Has(name string) bool {
	for _, t := range r.Tools {
		if t.Name == name {
			return t.Available()
		}
	}
	return false
}

// Missing returns the tools that are not here, most important first.
func (r Report) Missing() []Tool {
	var out []Tool
	for _, t := range r.Tools {
		if !t.Available() {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Need < out[j].Need })
	return out
}

// MissingRequired returns the tools without which the loop cannot work.
func (r Report) MissingRequired() []Tool {
	var out []Tool
	for _, t := range r.Missing() {
		if t.Need == Required {
			out = append(out, t)
		}
	}
	return out
}

// InstallHint suggests how to get what is missing. It names the packages
// rather than a command, because the command differs per distribution and
// guessing it wrong sends someone down the wrong path.
func InstallHint(missing []Tool) string {
	pkgs := map[string]string{
		"pdftotext": "poppler-utils",
		"grim":      "grim",
		"slurp":     "slurp",
		"wl-paste":  "wl-clipboard",
		"git":       "git",
	}
	seen := map[string]bool{}
	var names []string
	for _, t := range missing {
		if p, ok := pkgs[t.Name]; ok && !seen[p] {
			seen[p] = true
			names = append(names, p)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "packages to install: " + strings.Join(names, ", ") +
		"\n(or run the loop through 'nix develop' / 'nix run', which brings them)"
}
