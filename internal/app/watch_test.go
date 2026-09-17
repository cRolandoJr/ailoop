package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

func loopWatch(t *testing.T, s *state.AIState, checks []verify.Check) (*Loop, *[]string) {
	t.Helper()
	dir := t.TempDir()
	if err := state.Save(dir, s); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Verify: checks, TimeoutSeconds: 10}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	var etapas []string
	return NewLoop(dir, &modeloScript{}, nil, cfg), &etapas
}

func opcionesQueRegistran(etapas *[]string) AdvanceOptions {
	return AdvanceOptions{
		Review: apruebaSiempre,
		OnProgress: func(stage, detail string) {
			*etapas = append(*etapas, stage+": "+detail)
		},
	}
}

func TestUnaVerificacionQueNoCorrioNoEsUnaQuePaso(t *testing.T) {
	// Sin comandos declarados, Verify falla. Leer eso como verde seria el
	// pase vacuo que este proyecto existe para no tener, y aca ocurriria
	// sin nadie mirando.
	l, etapas := loopWatch(t, state.NewState("tarea"), nil)

	err := l.OnChange(context.Background(), opcionesQueRegistran(etapas))
	if err == nil {
		t.Fatal("no reporto que no pudo verificar")
	}
	todo := strings.Join(*etapas, " | ")
	if strings.Contains(strings.ToLower(todo), "passed") {
		t.Errorf("dijo que algo paso sin correr nada:\n%s", todo)
	}
}

func TestConChecksEnVerdeNoDespiertaAlAgente(t *testing.T) {
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseImplementation
	l, etapas := loopWatch(t, s, []verify.Check{{Name: "ok", Cmd: "exit 0"}})

	if err := l.OnChange(context.Background(), opcionesQueRegistran(etapas)); err != nil {
		t.Fatal(err)
	}
	todo := strings.Join(*etapas, " | ")
	if !strings.Contains(todo, "watch-pass") {
		t.Errorf("no informo que estaba todo bien:\n%s", todo)
	}
	if strings.Contains(todo, "watch-fail") {
		t.Errorf("desperto al agente con los checks en verde:\n%s", todo)
	}
}

func TestElWatchNoTeletransportaLaFase(t *testing.T) {
	// Escribir CurrentPhase a mano saltea los guards: dejaria el estado
	// afirmando aprobaciones que nadie dio.
	s := state.NewState("tarea") // arranca en DISCOVERY
	l, etapas := loopWatch(t, s, []verify.Check{{Name: "falla", Cmd: "exit 1"}})

	if err := l.OnChange(context.Background(), opcionesQueRegistran(etapas)); err != nil {
		t.Fatal(err)
	}

	despues, err := state.Load(l.Workspace())
	if err != nil {
		t.Fatal(err)
	}
	if despues.CurrentPhase != state.PhaseDiscovery {
		t.Errorf("movio la fase a %s sin pasar por ningun guard", despues.CurrentPhase)
	}
	if !strings.Contains(strings.Join(*etapas, " | "), "watch-skip") {
		t.Errorf("no explico por que no hizo nada:\n%s", strings.Join(*etapas, " | "))
	}
}
