package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

// modeloScript devuelve respuestas preparadas y cuenta las llamadas.
type modeloScript struct {
	respuestas []string
	llamadas   int
}

func (m *modeloScript) Describe() llm.Capabilities {
	return llm.Capabilities{Provider: "fake", Model: "script"}
}
func (m *modeloScript) Generate(context.Context, []llm.Message) (llm.Response, error) {
	r := "propuesta generica"
	if m.llamadas < len(m.respuestas) {
		r = m.respuestas[m.llamadas]
	}
	m.llamadas++
	return llm.Response{Text: r, Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}}, nil
}
func (m *modeloScript) GenerateStream(ctx context.Context, msgs []llm.Message, onChunk func(string)) (llm.Response, error) {
	return m.Generate(ctx, msgs)
}

// apruebaSiempre / rechazaSiempre: el humano, doblado.
func apruebaSiempre(ReviewRequest) ReviewDecision { return ReviewDecision{Approved: true} }
func rechazaCon(motivo string) func(ReviewRequest) ReviewDecision {
	return func(ReviewRequest) ReviewDecision {
		return ReviewDecision{Approved: false, Reason: motivo}
	}
}

func loopParaAvanzar(t *testing.T, s *state.AIState, checks []verify.Check, m llm.Client) *Loop {
	t.Helper()
	dir := t.TempDir()
	if err := state.Save(dir, s); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Verify: checks, TimeoutSeconds: 10}
	if err := config.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	return NewLoop(dir, m, nil, cfg)
}

func TestUnRechazoNoAvanzaYGuardaElMotivo(t *testing.T) {
	s := state.NewState("tarea")
	l := loopParaAvanzar(t, s, nil, &modeloScript{})

	out, err := l.Advance(context.Background(), AdvanceOptions{
		Review: rechazaCon("le falta el caso vacio"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Advanced {
		t.Error("avanzo pese al rechazo")
	}
	if !out.Rejected {
		t.Error("no marco el rechazo")
	}

	// El motivo tiene que estar archivado para la proxima vuelta.
	despues, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if despues.Phase != state.PhaseDiscovery {
		t.Errorf("la fase cambio: %s", despues.Phase)
	}
	h, _ := l.History()
	if len(h[0].Revisions) != 1 || h[0].Revisions[0].Rejected != "le falta el caso vacio" {
		t.Errorf("no archivo el motivo: %+v", h[0].Revisions)
	}
}

func TestAprobarAvanzaDeFase(t *testing.T) {
	l := loopParaAvanzar(t, state.NewState("tarea"), nil, &modeloScript{})

	out, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Advanced {
		t.Fatal("no avanzo")
	}
	if out.From != state.PhaseDiscovery || out.To != state.PhaseDesign {
		t.Errorf("transicion = %s -> %s", out.From, out.To)
	}
}

func TestSinCallbackDeRevisionNoCorre(t *testing.T) {
	// Nada avanza sin que un humano conteste.
	l := loopParaAvanzar(t, state.NewState("tarea"), nil, &modeloScript{})
	if _, err := l.Advance(context.Background(), AdvanceOptions{}); err == nil {
		t.Error("corrio sin Review")
	}
}

func TestDoneExigeLosChecksDelProyecto(t *testing.T) {
	// El corazon del protocolo: DONE lo otorga el proyecto, no el agente.
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseVerification
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}

	l := loopParaAvanzar(t, s, []verify.Check{{Name: "falla", Cmd: "exit 1"}}, &modeloScript{})

	out, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err != nil {
		t.Fatal(err)
	}
	if out.To == state.PhaseDone {
		t.Error("llego a DONE con los checks en rojo")
	}
	if out.Verification == nil || out.Verification.Passed {
		t.Errorf("no reporto la verificacion fallida: %+v", out.Verification)
	}
	// El gate rojo no cierra: manda el trabajo de vuelta a implementar.
	if out.AutoFixes != 1 {
		t.Errorf("AutoFixes = %d, quiero 1", out.AutoFixes)
	}
}

func TestElAutoFixSeAgotaEnElTecho(t *testing.T) {
	// Era el goto run_agent. Cada invocacion consume un intento, y el techo
	// se cuenta en el estado: un proyecto cuyos tests no pueden pasar deja de
	// quemar tokens en algun momento.
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseVerification
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}

	l := loopParaAvanzar(t, s, []verify.Check{{Name: "siempre-falla", Cmd: "exit 1"}},
		&modeloScript{})

	llevarAVerification := func(t *testing.T) {
		t.Helper()
		cur, err := state.Load(l.Workspace())
		if err != nil {
			t.Fatal(err)
		}
		if cur.CurrentPhase == state.PhaseVerification {
			return
		}
		if _, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre}); err != nil {
			t.Fatal(err)
		}
	}

	for intento := 1; intento <= MaxAutoFixes; intento++ {
		llevarAVerification(t)
		out, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
		if err != nil {
			t.Fatalf("intento %d: %v", intento, err)
		}
		if out.To == state.PhaseDone {
			t.Fatalf("intento %d: cerro con los checks rojos", intento)
		}
		if out.AutoFixes != intento {
			t.Errorf("intento %d: AutoFixes = %d", intento, out.AutoFixes)
		}
	}

	// Agotado el techo, el siguiente intento ya no repara: se rinde.
	llevarAVerification(t)
	out, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err != nil {
		t.Fatal(err)
	}
	if out.AutoFixes != 0 {
		t.Errorf("siguio reparando pasado el techo: AutoFixes = %d", out.AutoFixes)
	}
	if out.Advanced {
		t.Error("avanzo con los checks en rojo y el techo agotado")
	}
}

func TestDoneSeConcedeConLosChecksEnVerde(t *testing.T) {
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseVerification
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}

	l := loopParaAvanzar(t, s, []verify.Check{{Name: "ok", Cmd: "exit 0"}}, &modeloScript{})

	out, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Advanced || out.To != state.PhaseDone {
		t.Errorf("no cerro con los checks verdes: %+v", out)
	}
}

func TestElGuardBloqueaYDiceQueFalta(t *testing.T) {
	// Sin Design aprobado no se entra a IMPLEMENTATION.
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhasePlan
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "producido, sin aprobar"}

	l := loopParaAvanzar(t, s, nil, &modeloScript{})

	_, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err == nil {
		t.Fatal("dejo pasar el guard")
	}
	var ge *GuardError
	if !asGuardError(err, &ge) {
		t.Fatalf("err = %T, quiero *GuardError", err)
	}
	if len(ge.Missing) == 0 {
		t.Error("no dijo que faltaba")
	}
	if !strings.Contains(strings.Join(ge.Missing, " "), "Design") {
		t.Errorf("Missing = %v, quiero que mencione el Design", ge.Missing)
	}
}

func asGuardError(err error, target **GuardError) bool {
	g, ok := err.(*GuardError)
	if ok {
		*target = g
	}
	return ok
}

func TestElCriticNoBloqueaSiSeRompe(t *testing.T) {
	// Un critic que falla no puede frenar el trabajo: el humano decide igual.
	l := loopParaAvanzar(t, state.NewState("tarea"), nil,
		&modeloScript{respuestas: []string{"propuesta", "esto no es JSON valido"}})

	out, err := l.Advance(context.Background(), AdvanceOptions{
		UseCritic: true,
		Review:    apruebaSiempre,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Advanced {
		t.Error("el critic roto bloqueo el avance")
	}
}

func TestUndoVuelveAtrasYReabreLaFase(t *testing.T) {
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhasePlan
	s.Design = state.Document{Version: 1, Content: "diseno", Approved: true}

	l := loopParaAvanzar(t, s, nil, &modeloScript{})
	out, err := l.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if out.To != state.PhaseDesign {
		t.Errorf("volvio a %s, quiero DESIGN", out.To)
	}

	// La aprobacion no sobrevive: nadie volvio a mirar ese diseno.
	st, _ := l.Status()
	for _, a := range st.Artifacts {
		if a.Name == "Design" && a.Approved {
			t.Error("el Design quedo aprobado despues del undo")
		}
	}
}

func TestUndoEnLaPrimeraFaseAvisaEnVezDeRomper(t *testing.T) {
	l := loopParaAvanzar(t, state.NewState("tarea"), nil, &modeloScript{})
	out, err := l.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if !out.AtFirstPhase {
		t.Error("no aviso que ya estaba en la primera fase")
	}
}

func TestNoSeParcheaUnArchivoQueElAgenteNoAbrio(t *testing.T) {
	// El invariante de punta a punta: el agente propone un parche sobre un
	// archivo que nunca leyo, y el loop lo frena antes de tocar el disco.
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseImplementation
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}

	parche := "<<<<\nobjetivo.go\n====\nvalor viejo\n====\nvalor nuevo\n>>>>\n"
	l := loopParaAvanzar(t, s, nil, &modeloScript{respuestas: []string{parche}})

	original := "valor viejo\n"
	destino := filepath.Join(l.Workspace(), "objetivo.go")
	if err := os.WriteFile(destino, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := l.Advance(context.Background(), AdvanceOptions{
		Review: func(r ReviewRequest) ReviewDecision {
			return ReviewDecision{Approved: true, ApprovedBlocks: r.Blocks}
		},
	})
	if err == nil {
		t.Fatal("aplico un parche sobre un archivo que el agente nunca leyo")
	}
	if !strings.Contains(err.Error(), "never read") {
		t.Errorf("err = %v, quiero que mencione que no lo leyo", err)
	}

	data, _ := os.ReadFile(destino)
	if string(data) != original {
		t.Errorf("el archivo se modifico pese al rechazo: %q", string(data))
	}
}

func TestHabiendoLeidoElParcheSiEntra(t *testing.T) {
	s := state.NewState("tarea")
	s.CurrentPhase = state.PhaseImplementation
	s.Spec = state.Document{Version: 1, Content: "s", Approved: true}
	s.Design = state.Document{Version: 1, Content: "d", Approved: true}
	s.Plan = state.Document{Version: 1, Content: "p", Approved: true}
	// El agente ya habia leido el archivo en una fase anterior.
	s.NoteFileRead("objetivo.go")

	parche := "<<<<\nobjetivo.go\n====\nvalor viejo\n====\nvalor nuevo\n>>>>\n"
	l := loopParaAvanzar(t, s, []verify.Check{{Name: "ok", Cmd: "exit 0"}},
		&modeloScript{respuestas: []string{parche}})

	destino := filepath.Join(l.Workspace(), "objetivo.go")
	if err := os.WriteFile(destino, []byte("valor viejo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Advance(context.Background(), AdvanceOptions{
		Review: func(r ReviewRequest) ReviewDecision {
			return ReviewDecision{Approved: true, ApprovedBlocks: r.Blocks}
		},
	}); err != nil {
		t.Fatalf("rechazo un parche legitimo: %v", err)
	}

	data, _ := os.ReadFile(destino)
	if string(data) != "valor nuevo\n" {
		t.Errorf("no aplico el parche: %q", string(data))
	}
}

func TestElPresupuestoCortaAntesDeLlamarAlModelo(t *testing.T) {
	// Chequear despues de gastar produce un recibo, no un limite.
	s := state.NewState("tarea")
	s.Budget = state.Budget{MaxTokens: 100}
	s.Spend.Record(state.PhaseDiscovery, llm.Usage{InputTokens: 90, OutputTokens: 20})

	m := &modeloScript{}
	l := loopParaAvanzar(t, s, nil, m)

	_, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err == nil {
		t.Fatal("avanzo con el presupuesto agotado")
	}
	if !errors.Is(err, state.ErrOverBudget) {
		t.Errorf("err = %v, quiero ErrOverBudget", err)
	}
	if m.llamadas != 0 {
		t.Errorf("llamo al modelo %d veces con el techo agotado", m.llamadas)
	}
}

func TestConPresupuestoDisponibleAvanzaNormal(t *testing.T) {
	s := state.NewState("tarea")
	s.Budget = state.Budget{MaxTokens: 100000}

	l := loopParaAvanzar(t, s, nil, &modeloScript{})
	out, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre})
	if err != nil {
		t.Fatalf("no avanzo teniendo presupuesto: %v", err)
	}
	if !out.Advanced {
		t.Error("no avanzo")
	}
}
