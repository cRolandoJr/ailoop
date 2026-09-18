package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

func implementando(t *testing.T) *state.AIState {
	t.Helper()
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseImplementation
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}
	s.NoteFileRead("objetivo.go")
	return s
}

// El ensayo en seco cuesta cero tokens y cero ejecucion, asi que la persona
// tiene derecho a verlo ANTES de decidir, no como error despues de aprobar.
func TestLaRevisionVeElBloqueQueNoVaAAplicar(t *testing.T) {
	parche := "<<<<\nobjetivo.go\n====\ncontexto viejo\n====\nvalor nuevo\n>>>>\n"
	l := loopParaAvanzar(t, implementando(t), nil, &modeloScript{respuestas: []string{parche}})

	destino := filepath.Join(l.Workspace(), "objetivo.go")
	if err := os.WriteFile(destino, []byte("lo que dice de verdad\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var visto ReviewRequest
	_, _ = l.Advance(context.Background(), AdvanceOptions{
		Review: func(r ReviewRequest) ReviewDecision {
			visto = r
			return ReviewDecision{Approved: false, Reason: "no"}
		},
	})

	if len(visto.Problems) != 1 {
		t.Fatalf("la revision vio %d problemas, quiero 1: %+v", len(visto.Problems), visto.Problems)
	}
	if !strings.Contains(visto.Problems[0].Err.Error(), "search block not found") {
		t.Errorf("el problema dice %v, quiero que nombre el SEARCH que no matchea", visto.Problems[0].Err)
	}
}

// Control positivo: un parche sano no puede producir ruido, o la senal no
// sirve para nada.
func TestLaRevisionNoInventaProblemasConUnParcheSano(t *testing.T) {
	parche := "<<<<\nobjetivo.go\n====\nvalor viejo\n====\nvalor nuevo\n>>>>\n"
	l := loopParaAvanzar(t, implementando(t), []verify.Check{{Name: "ok", Cmd: "exit 0"}},
		&modeloScript{respuestas: []string{parche}})

	destino := filepath.Join(l.Workspace(), "objetivo.go")
	if err := os.WriteFile(destino, []byte("valor viejo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var visto ReviewRequest
	_, err := l.Advance(context.Background(), AdvanceOptions{
		Review: func(r ReviewRequest) ReviewDecision {
			visto = r
			return ReviewDecision{Approved: true, ApprovedBlocks: r.Blocks}
		},
	})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if len(visto.Problems) != 0 {
		t.Fatalf("invento problemas sobre un parche sano: %+v", visto.Problems)
	}
}
