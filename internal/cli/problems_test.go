package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/patch"
)

func TestProblemLinesNamesThePatchTheFileAndTheReason(t *testing.T) {
	blocks := []patch.Block{{FilePath: "a.go"}, {FilePath: "b.go"}, {FilePath: "c.go"}}
	problems := []patch.Problem{
		{Index: 1, File: "b.go", Err: errors.New("search block not found in file")},
	}

	lines := ProblemLines(blocks, problems)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), lines)
	}
	for _, want := range []string{"2/3", "b.go", "search block not found"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line %q does not mention %q", lines[0], want)
		}
	}
}

func TestProblemLinesIsEmptyWhenEveryBlockApplies(t *testing.T) {
	blocks := []patch.Block{{FilePath: "a.go"}}
	if lines := ProblemLines(blocks, nil); len(lines) != 0 {
		t.Fatalf("got %q, want nothing to report", lines)
	}
}
