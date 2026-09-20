// Package cli renders what the use cases return.
//
// This package is deliberately dumb - it formats and prints, and decides
// nothing. That is the point: a layer with no decisions in it needs no tests,
// and every decision worth testing ends up in internal/app where it can be.
//
// The pattern has a name: humble object.
package cli

import (
	"fmt"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/app"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/pterm/pterm"
)

func Error(err error) {
	pterm.Error.Printf("%v\n", err)
	if hint := ShellHint(err); hint != "" {
		pterm.Info.Println(hint)
	}
}

func Status(r *app.StatusReport) {
	fmt.Printf("Current AI Loop status:\n")
	fmt.Printf("  Task:  %s\n", r.Task)
	fmt.Printf("  Phase: %s (v%d)\n", r.Phase, r.Version)
	for _, a := range r.Artifacts {
		fmt.Printf("    %-8s v%d  %s\n", a.Name, a.Version, artifactMark(a))
	}
}

func artifactMark(a app.ArtifactState) string {
	switch {
	case a.Approved:
		return "approved"
	case a.Produced:
		return "produced, NOT approved"
	default:
		return "pending"
	}
}

func Cost(l *state.Ledger, b state.Budget) {
	fmt.Print(l.Report())
	if b.Set() {
		fmt.Printf("\nBudget: %s\n", b.Remaining(l))
	}
}

func History(hs []app.ArtifactHistory) {
	for _, h := range hs {
		fmt.Printf("%s: v%d current, %d archived revision(s)\n",
			h.Name, h.CurrentVersion, len(h.Revisions))
		for _, r := range h.Revisions {
			reason := r.Rejected
			if reason == "" {
				reason = "(no reason recorded)"
			}
			fmt.Printf("    v%d  %s  rejected: %s\n",
				r.Version, r.At.Format("2006-01-02 15:04"), reason)
		}
	}
}

func Decisions(ds []state.Decision) {
	if len(ds) == 0 {
		pterm.Info.Println("No decisions recorded yet.")
		return
	}
	for _, d := range ds {
		mark := "active"
		if !d.Active() {
			mark = "superseded by " + d.SupersededBy
		}
		fmt.Printf("%s v%d [%s] %s (%s)\n", d.ID, d.Version, mark, d.Statement, d.Phase)
		if d.Rationale != "" {
			fmt.Printf("      why: %s\n", d.Rationale)
		}
	}
}

func DecisionRecorded(d state.Decision) {
	pterm.Success.Printf("%s recorded: %s\n", d.ID, d.Statement)
}

func Capabilities(r *app.CapabilitiesReport) {
	fmt.Printf("Provider: %s\n", r.Model.Provider)
	fmt.Printf("Model:    %s\n", r.Model.Model)
	fmt.Printf("Vision:   %s\n", r.Model.Vision)
	fmt.Printf("Thinking: %s\n", r.Model.Thinking)
	if r.Model.MaxContextTokens > 0 {
		fmt.Printf("Context:  %d tokens\n", r.Model.MaxContextTokens)
	} else {
		fmt.Printf("Context:  unknown\n")
	}

	fmt.Println()
	fmt.Println("Capabilities offered to agents, by phase:")
	for _, g := range r.ByPhase {
		var names []string
		for _, c := range g.Granted {
			names = append(names, string(c))
		}
		// With phase routing, WHO serves the phase is part of the answer:
		// grants are computed against that client, not against the default.
		who := ""
		if g.Provider != "" && (g.Provider != r.Model.Provider || g.Model != r.Model.Model) {
			who = fmt.Sprintf(" [%s %s]", g.Provider, g.Model)
		}
		fmt.Printf("  %-16s%s %s\n", g.Phase, who, strings.Join(names, ", "))
	}
}
