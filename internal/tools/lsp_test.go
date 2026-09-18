package tools

import (
	"context"
	"strings"
	"testing"
)

// fakeLSP records what the registry actually asked the language server, so a
// test can assert that a refused request never reached it.
type fakeLSP struct {
	calls  []string
	result string
}

func (f *fakeLSP) Definition(ctx context.Context, path string, line, char int) (string, error) {
	f.calls = append(f.calls, path)
	return f.result, nil
}

func (f *fakeLSP) References(ctx context.Context, path string, line, char int) (string, error) {
	f.calls = append(f.calls, path)
	return f.result, nil
}

func lspRegistry(f *fakeLSP) *Registry {
	return &Registry{
		Allowed: map[Capability]bool{LSPDefinition: true, LSPReferences: true},
		LSP:     f,
	}
}

// A line number that is not a number is not line zero. Reading it as zero
// answers a question nobody asked, and says nothing went wrong.
func TestLSPRejectsANonNumericPosition(t *testing.T) {
	f := &fakeLSP{result: "unreachable"}
	res := Execute(context.Background(), t.TempDir(), lspRegistry(f),
		Request{Cap: LSPDefinition, Arg: "main.go:abc:xyz"})

	if res.Err == nil {
		t.Fatalf("got output %q with no error, want a parse error", res.Output)
	}
	if len(f.calls) != 0 {
		t.Fatalf("the language server was called anyway: %v", f.calls)
	}
}

// Every other filesystem capability resolves its argument through
// patch.SafeRelPath. A path that leaves the workspace must not reach the
// server through this one either.
func TestLSPRefusesAPathOutsideTheWorkspace(t *testing.T) {
	for _, arg := range []string{"../../../etc/passwd:1:1", "/etc/passwd:1:1"} {
		f := &fakeLSP{result: "unreachable"}
		res := Execute(context.Background(), t.TempDir(), lspRegistry(f),
			Request{Cap: LSPReferences, Arg: arg})

		if res.Err == nil {
			t.Fatalf("%s: got output %q with no error, want a refusal", arg, res.Output)
		}
		if len(f.calls) != 0 {
			t.Fatalf("%s: the language server was called anyway: %v", arg, f.calls)
		}
	}
}

// The position is the last two fields, so a path may contain a colon.
func TestLSPPassesAnAbsolutePathInsideTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	f := &fakeLSP{result: `[{"uri":"file:///x"}]`}
	res := Execute(context.Background(), t.TempDir(), lspRegistry(f),
		Request{Cap: LSPDefinition, Arg: "internal/app/loop.go:12:4"})
	_ = ws

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if len(f.calls) != 1 || !strings.HasSuffix(f.calls[0], "internal/app/loop.go") {
		t.Fatalf("server saw %v, want the workspace-resolved path", f.calls)
	}
}
