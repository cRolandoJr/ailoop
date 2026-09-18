package state

import (
	"github.com/cRolandoJr/ailoop/internal/env"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentVersionSigueALaFase(t *testing.T) {
	s := NewState("tarea")
	s.Spec.Version = 3
	s.Design.Version = 7

	if got := s.CurrentVersion(); got != 3 {
		t.Errorf("en DISCOVERY, CurrentVersion = %d, quiero 3 (la del Spec)", got)
	}

	s.CurrentPhase = PhaseDesign
	if got := s.CurrentVersion(); got != 7 {
		t.Errorf("en DESIGN, CurrentVersion = %d, quiero 7", got)
	}
}

func TestCurrentDocumentEsElArtefactoQueEscribeLaFase(t *testing.T) {
	s := NewState("tarea")
	casos := map[Phase]*Document{
		PhaseDiscovery:      &s.Spec,
		PhaseDesign:         &s.Design,
		PhasePlan:           &s.Plan,
		PhaseImplementation: &s.Decisions,
		PhaseVerification:   &s.Decisions,
	}
	for fase, quiero := range casos {
		s.CurrentPhase = fase
		if got := s.CurrentDocument(); got != quiero {
			t.Errorf("en %s, CurrentDocument apunta al artefacto equivocado", fase)
		}
	}
}

func TestEnDoneNoHayArtefactoCorriente(t *testing.T) {
	s := NewState("tarea")
	s.CurrentPhase = PhaseDone
	if d := s.CurrentDocument(); d != nil {
		t.Errorf("DONE devolvio un documento: %+v", d)
	}
	if v := s.CurrentVersion(); v != 1 {
		t.Errorf("CurrentVersion en DONE = %d, quiero el default 1", v)
	}
}

func TestUnEstadoViejoSigueCargandoElFeedback(t *testing.T) {
	// El campo se llamaba RejectionReason. Al renombrarlo se mantuvo la clave
	// JSON, porque cualquier .ailoop/state.json escrito antes del cambio la
	// usa - y perder ese texto en silencio le borraria al agente la unica
	// correccion que la persona escribio.
	dir := t.TempDir()
	viejo := `{
	  "task_description": "tarea previa al rename",
	  "current_phase": "DISCOVERY",
	  "rejection_reason": "le falta el caso vacio"
	}`
	ailoopDir := env.AILoopDir(dir)
	if err := os.MkdirAll(ailoopDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ailoopDir, "state.json"), []byte(viejo), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Feedback != "le falta el caso vacio" {
		t.Errorf("Feedback = %q, se esperaba el texto de rejection_reason", s.Feedback)
	}
}

func TestElFeedbackSeGuardaConLaClaveVieja(t *testing.T) {
	// El otro sentido: una version anterior de ailoop debe poder leer lo que
	// esta escribe.
	dir := t.TempDir()
	s := NewState("tarea")
	s.Feedback = "revisá el borde superior"
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(env.AILoopDir(dir), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"rejection_reason"`) {
		t.Errorf("el JSON no usa la clave vieja:\n%s", raw)
	}
	if strings.Contains(string(raw), `"feedback"`) {
		t.Errorf("el JSON usa una clave nueva, que rompe a las versiones anteriores:\n%s", raw)
	}
}
