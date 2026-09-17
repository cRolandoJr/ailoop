package state

import "testing"

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
