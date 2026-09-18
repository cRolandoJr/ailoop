package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The scripted server proves the framing and the routing. It cannot prove the
// handshake is the one a real server expects, so this asks gopls.
func TestAgainstRealGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not on PATH")
	}

	ws := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(ws, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("go.mod", "module probe\n\ngo 1.21\n")
	main := write("main.go", `package main

func Greet() string { return "hi" }

func main() { _ = Greet() }
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := Start(ctx, "gopls")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(ctx, ws); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Line 4, character 18 is the call to Greet; the definition is on line 2.
	got, err := c.Definition(ctx, main, 4, 18)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	t.Logf("gopls answered: %s", got)

	if !strings.Contains(got, "main.go") {
		t.Fatalf("got %q, want a location in main.go", got)
	}
	if !strings.Contains(got, `"line":2`) {
		t.Fatalf("got %q, want the definition on line 2", got)
	}
}
