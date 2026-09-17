package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProjectContext is what the agent is told about the project before it starts.
type ProjectContext struct {
	Text string
	// Named are the files the user flagged and that exist.
	Named []string
	// Missing are the ones they flagged that do not.
	Missing []string
	// Inlined is true when the file contents were pasted in full.
	Inlined bool
}

// BuildProjectContext prepares what the agent sees about the workspace.
//
// Naming the files instead of pasting them is the default because the agent
// can read what it needs: pasting five files into every request of every round
// pays for four of them that were never opened. inline keeps the old behaviour
// for a model with no tool budget to spare.
func (l *Loop) BuildProjectContext(files []string, inline bool) (*ProjectContext, error) {
	pc := &ProjectContext{Inlined: inline}

	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, err := os.Stat(l.resolve(f)); err != nil {
			pc.Missing = append(pc.Missing, f)
			continue
		}
		pc.Named = append(pc.Named, f)
	}

	var b strings.Builder
	switch {
	case inline:
		for _, f := range pc.Named {
			content, err := os.ReadFile(l.resolve(f))
			if err != nil {
				pc.Missing = append(pc.Missing, f)
				continue
			}
			fmt.Fprintf(&b, "\n--- FILE: %s ---\n%s\n", f, string(content))
		}
	case len(pc.Named) > 0:
		b.WriteString("Files the user flagged as relevant:\n  ")
		b.WriteString(strings.Join(pc.Named, "\n  "))
		b.WriteString("\nRead the ones you actually need with fs.read; do not assume their contents.")
	}

	// Uncommitted work is context the agent needs: it should not step on
	// changes the user has not saved yet.
	if dirty := l.uncommittedChanges(); dirty != "" {
		fmt.Fprintf(&b, "\n<GIT_CONTEXT>\nUncommitted changes in this repository:\n%s\n</GIT_CONTEXT>\n", dirty)
	}

	// Project conventions, if the repository states any. An agent that never
	// sees them violates them without knowing they exist.
	if conv, name := l.conventions(); conv != "" {
		fmt.Fprintf(&b, "\n<PROJECT_CONVENTIONS source=%q>\n%s\n</PROJECT_CONVENTIONS>\n", name, conv)
	}

	pc.Text = b.String()
	return pc, nil
}

func (l *Loop) resolve(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(l.workspace, p)
}

func (l *Loop) uncommittedChanges() string {
	cmd := exec.Command("git", "status", "-s")
	cmd.Dir = l.workspace
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// conventionFiles are where projects usually state their rules, most specific
// first.
var conventionFiles = []string{"CLAUDE.md", "AGENTS.md", "CONVENTIONS.md", "CONTRIBUTING.md"}

// maxConventionBytes bounds what gets injected. A CONTRIBUTING.md can be a
// small book, and it travels in every request of every round.
const maxConventionBytes = 8000

func (l *Loop) conventions() (text, source string) {
	for _, name := range conventionFiles {
		data, err := os.ReadFile(filepath.Join(l.workspace, name))
		if err != nil {
			continue
		}
		if len(data) > maxConventionBytes {
			return string(data[:maxConventionBytes]) + "\n... (truncated)", name
		}
		return string(data), name
	}
	return "", ""
}
