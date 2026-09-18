package app

import (
	"context"
	"testing"
	"time"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// Estas pruebas existen por el refactor. La misma logica, mientras vivia
// dentro del switch de main(), no se podia ejercitar sin arrancar el CLI.

type modeloFalso struct{ caps llm.Capabilities }

func (m *modeloFalso) Describe() llm.Capabilities { return m.caps }
func (m *modeloFalso) Generate(context.Context, []llm.Message) (llm.Response, error) {
	return llm.Response{Text: "ok"}, nil
}
func (m *modeloFalso) GenerateStream(ctx context.Context, msgs []llm.Message, onChunk func(string)) (llm.Response, error) {
	return m.Generate(ctx, msgs)
}

func loopCon(t *testing.T, s *state.AIState, client llm.Client) *Loop {
	t.Helper()
	dir := t.TempDir()
	if s != nil {
		if err := state.Save(dir, s); err != nil {
			t.Fatal(err)
		}
	}
	return NewLoop(dir, client, nil, nil, nil)
}

func TestStatusDistingueProducidoDeAprobado(t *testing.T) {
	// Es la distincion central del protocolo, y hasta ahora solo se podia
	// comprobar mirando la terminal.
	s := state.NewState("mi tarea")
	s.Spec = state.Document{Version: 2, Content: "hay texto", Approved: true}
	s.Design = state.Document{Version: 1, Content: "producido pero sin aprobar"}

	r, err := loopCon(t, s, nil).Status()
	if err != nil {
		t.Fatal(err)
	}

	if r.Task != "mi tarea" {
		t.Errorf("Task = %q", r.Task)
	}
	if r.Version != 2 {
		t.Errorf("Version = %d, quiero la del Spec (2)", r.Version)
	}

	porNombre := map[string]ArtifactState{}
	for _, a := range r.Artifacts {
		porNombre[a.Name] = a
	}
	if spec := porNombre["Spec"]; !spec.Approved || !spec.Produced {
		t.Errorf("Spec = %+v, quiero producido y aprobado", spec)
	}
	if d := porNombre["Design"]; d.Approved {
		t.Errorf("Design figura aprobado y nadie lo aprobo: %+v", d)
	} else if !d.Produced {
		t.Errorf("Design figura no producido y tiene contenido: %+v", d)
	}
}

func TestSinLoopIniciadoSeAvisaEnVezDeExplotar(t *testing.T) {
	// Antes esto era un os.Exit(1) dentro del case: imposible de verificar.
	_, err := NewLoop(t.TempDir(), nil, nil, nil, nil).Status()
	if err == nil {
		t.Fatal("no aviso que no hay loop iniciado")
	}
}

func TestDecideRegistraYPersiste(t *testing.T) {
	l := loopCon(t, state.NewState("tarea"), nil)

	d, err := l.Decide("el config va en JSON", "cero dependencias")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Statement != "el config va en JSON" {
		t.Errorf("decision devuelta = %+v", d)
	}

	// Una segunda lectura tiene que verla: el caso de uso guarda, no solo
	// devuelve.
	ds, err := l.Decisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Rationale != "cero dependencias" {
		t.Errorf("no persistio: %+v", ds)
	}
}

func TestHistoryDevuelveLasRevisionesArchivadas(t *testing.T) {
	s := state.NewState("tarea")
	s.Spec.Content = "v1"
	s.Spec.RejectProposal("propuesta rechazada", "faltaba el caso vacio", time.Now())

	h, err := loopCon(t, s, nil).History()
	if err != nil {
		t.Fatal(err)
	}
	var spec ArtifactHistory
	for _, a := range h {
		if a.Name == "Spec" {
			spec = a
		}
	}
	if len(spec.Revisions) != 1 {
		t.Fatalf("Revisions = %d, quiero 1", len(spec.Revisions))
	}
	if spec.Revisions[0].Rejected != "faltaba el caso vacio" {
		t.Errorf("no conservo el motivo: %q", spec.Revisions[0].Rejected)
	}
}

func TestCapabilitiesSinModeloLoDiceClaro(t *testing.T) {
	_, err := loopCon(t, state.NewState("t"), nil).Capabilities()
	if err != ErrNoClient {
		t.Errorf("err = %v, quiero ErrNoClient", err)
	}
}

func TestCapabilitiesCruzaModeloConFase(t *testing.T) {
	// Sin vision, fs.read_image no se ofrece en ninguna fase. Fail-closed.
	ciego := &modeloFalso{caps: llm.Capabilities{Vision: llm.Unsupported}}
	r, err := loopCon(t, state.NewState("t"), ciego).Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ByPhase) != len(state.Phases()) {
		t.Fatalf("ByPhase = %d fases, quiero %d", len(r.ByPhase), len(state.Phases()))
	}
	for _, g := range r.ByPhase {
		for _, c := range g.Granted {
			if c == tools.FSReadImage {
				t.Errorf("%s ofrece fs.read_image a un modelo que no ve", g.Phase)
			}
		}
	}

	vidente := &modeloFalso{caps: llm.Capabilities{Vision: llm.Supported}}
	r2, err := loopCon(t, state.NewState("t"), vidente).Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	var ofrecida bool
	for _, g := range r2.ByPhase {
		for _, c := range g.Granted {
			if c == tools.FSReadImage {
				ofrecida = true
			}
		}
	}
	if !ofrecida {
		t.Error("con vision, ninguna fase ofrece fs.read_image")
	}
}

func TestCmdRunSoloEnVerification(t *testing.T) {
	// El que implementa no puede correr sus propios tests.
	m := &modeloFalso{caps: llm.Capabilities{Vision: llm.Supported}}
	r, err := loopCon(t, state.NewState("t"), m).Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range r.ByPhase {
		tiene := false
		for _, c := range g.Granted {
			if c == tools.CmdRun {
				tiene = true
			}
		}
		quiero := g.Phase == state.PhaseVerification
		if tiene != quiero {
			t.Errorf("%s: cmd.run = %v, quiero %v", g.Phase, tiene, quiero)
		}
	}
}
