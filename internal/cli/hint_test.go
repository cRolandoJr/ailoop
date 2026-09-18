package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/state"
)

// El consejo de la terminal lo pone la superficie de la terminal, que es la
// unica donde es cierto.
func TestShellHintExplicaComoEmpezar(t *testing.T) {
	got := ShellHint(state.ErrNoActiveLoop)
	if !strings.Contains(got, "ailoop start") {
		t.Errorf("ShellHint = %q, quiero que diga como empezar", got)
	}
}

func TestShellHintCallaParaCualquierOtroError(t *testing.T) {
	if got := ShellHint(errors.New("se cayo la red")); got != "" {
		t.Errorf("ShellHint = %q, quiero silencio", got)
	}
}
