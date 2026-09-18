package state

import (
	"testing"
	"time"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

// `ailoop start` pisaba el loop anterior sin preguntar. Para poder negarse hay
// que poder distinguir un loop recien creado -que no se pierde nada- de uno
// con trabajo adentro.
func TestUnLoopReciencreadoNoTieneTrabajo(t *testing.T) {
	if NewState("tarea").HasWork() {
		t.Error("un loop sin nada dice que tiene trabajo, y eso bloquearia empezar")
	}
}

func TestUnDocumentoAprobadoEsTrabajo(t *testing.T) {
	s := NewState("tarea")
	s.Spec = Document{Version: 1, Content: "algo", Approved: true}
	if !s.HasWork() {
		t.Error("una spec aprobada no cuenta como trabajo")
	}
}

func TestElGastoEsTrabajo(t *testing.T) {
	s := NewState("tarea")
	s.Spend.Record(PhaseDiscovery, llm.Usage{InputTokens: 1200, OutputTokens: 300})
	if !s.HasWork() {
		t.Error("tokens ya pagados no cuentan como trabajo")
	}
}

// Una propuesta rechazada tambien es trabajo: costo tokens y quedo en el
// historial como antecedente.
func TestUnaPropuestaRechazadaEsTrabajo(t *testing.T) {
	s := NewState("tarea")
	s.RejectCurrentProposal("propuesta", "no", time.Now())
	if !s.HasWork() {
		t.Error("una propuesta rechazada no cuenta como trabajo")
	}
}
