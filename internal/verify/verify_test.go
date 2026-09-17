package verify

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunPassingCheck(t *testing.T) {
	results := Run(context.Background(), ".", []Check{
		{Name: "ok", Cmd: "exit 0"},
	}, 10*time.Second)

	if len(results) != 1 {
		t.Fatalf("quiero 1 resultado, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("Passed = false, quiero true (exit=%d, output=%q)", results[0].ExitCode, results[0].Output)
	}
}

func TestRunFailingCheckIsNotAPass(t *testing.T) {
	results := Run(context.Background(), ".", []Check{
		{Name: "falla", Cmd: "exit 3"},
	}, 10*time.Second)

	if results[0].Passed {
		t.Errorf("Passed = true para un comando que salio con 3, quiero false")
	}
	if results[0].ExitCode != 3 {
		t.Errorf("ExitCode = %d, quiero 3", results[0].ExitCode)
	}
}

func TestRunCapturesStderr(t *testing.T) {
	results := Run(context.Background(), ".", []Check{
		{Name: "stderr", Cmd: "echo boom >&2; exit 1"},
	}, 10*time.Second)

	if !strings.Contains(results[0].Output, "boom") {
		t.Errorf("Output = %q, quiero que contenga lo que fue a stderr", results[0].Output)
	}
}

func TestAllPassedOnEmptyIsFalse(t *testing.T) {
	// "no corrio nada" NO puede leerse como "todo bien": es el modo de falla
	// que hace que un CI verde no pruebe nada.
	if AllPassed(nil) {
		t.Errorf("AllPassed(nil) = true, quiero false")
	}
}

func TestAllPassedNeedsEveryCheck(t *testing.T) {
	mixed := []Result{{Passed: true}, {Passed: false}}
	if AllPassed(mixed) {
		t.Errorf("AllPassed con un check rojo = true, quiero false")
	}
	if n := len(Failed(mixed)); n != 1 {
		t.Errorf("Failed devolvio %d, quiero 1", n)
	}
}
