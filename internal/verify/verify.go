// Package verify runs the project's own commands and reports what actually
// happened. It is the deterministic half of the enforcement model: the LLM
// proposes, this package checks the ground.
package verify

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Check is one verification command declared by the project.
type Check struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}

// Result is what actually happened when a Check ran.
type Result struct {
	Name     string
	Cmd      string
	Passed   bool
	ExitCode int
	Output   string
	Duration time.Duration
	// Err is set only when the command could not be started at all.
	Err error
}

const maxOutput = 8000

// Run executes every check in dir and returns one Result per check, in order.
// A check passes only when its command exits with status 0.
func Run(ctx context.Context, dir string, checks []Check, timeout time.Duration) []Result {
	results := make([]Result, 0, len(checks))

	for _, c := range checks {
		cctx, cancel := context.WithTimeout(ctx, timeout)

		start := time.Now()
		// sh -c so the project can declare its command the same way it types it.
		cmd := exec.CommandContext(cctx, "sh", "-c", c.Cmd)
		cmd.Dir = dir
		// CombinedOutput: a failing build writes to stderr, and hiding stderr is
		// how a "command not found" gets mistaken for a passing check.
		out, err := cmd.CombinedOutput()
		elapsed := time.Since(start)
		cancel()

		r := Result{
			Name:     c.Name,
			Cmd:      c.Cmd,
			Output:   truncate(string(out)),
			Duration: elapsed,
		}

		switch e := err.(type) {
		case nil:
			r.Passed = true
		case *exec.ExitError:
			r.ExitCode = e.ExitCode()
		default:
			// Could not start the command at all: not a failing check, a broken one.
			r.ExitCode = -1
			r.Err = err
		}

		results = append(results, r)
	}

	return results
}

// AllPassed reports whether every result passed. An empty slice is NOT a pass:
// "nothing ran" must never read as "everything is fine".
func AllPassed(results []Result) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// Failed returns only the results that did not pass.
func Failed(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

func truncate(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n... (output truncated)"
}
