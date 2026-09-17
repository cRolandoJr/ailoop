package state

import "testing"

func nuevoEn(fase Phase) *AIState {
	s := NewState("tarea")
	s.CurrentPhase = fase
	return s
}

func TestNoSePuedeEntrarAImplementationSinDesignAprobado(t *testing.T) {
	s := nuevoEn(PhasePlan)
	s.Spec = Document{Version: 1, Content: "spec", Approved: true}
	s.Design = Document{Version: 1, Content: "design"} // producido pero NO aprobado
	s.Plan = Document{Version: 1, Content: "plan", Approved: true}

	if err := CanTransition(s, PhaseImplementation); err == nil {
		t.Fatalf("la transicion fue permitida con el Design sin aprobar")
	}
}

func TestContenidoSinAprobacionNoAlcanza(t *testing.T) {
	// La distincion central: que un agente haya producido texto no es que un
	// humano lo haya aceptado.
	s := nuevoEn(PhaseDiscovery)
	s.Spec = Document{Version: 1, Content: "hay texto"}

	if err := CanTransition(s, PhaseDesign); err == nil {
		t.Errorf("permitio avanzar con contenido pero sin aprobacion")
	}

	s.Spec.Approved = true
	if err := CanTransition(s, PhaseDesign); err != nil {
		t.Errorf("rechazo una transicion valida: %v", err)
	}
}

func TestAprobacionSinContenidoTampocoAlcanza(t *testing.T) {
	s := nuevoEn(PhaseDiscovery)
	s.Spec = Document{Version: 1, Approved: true} // aprobado pero vacio

	if err := CanTransition(s, PhaseDesign); err == nil {
		t.Errorf("permitio avanzar con un artefacto vacio")
	}
}

func TestNoSePuedenSaltearFases(t *testing.T) {
	s := nuevoEn(PhaseDiscovery)
	s.Spec = Document{Version: 1, Content: "spec", Approved: true}
	s.Design = Document{Version: 1, Content: "d", Approved: true}
	s.Plan = Document{Version: 1, Content: "p", Approved: true}

	if err := CanTransition(s, PhaseImplementation); err == nil {
		t.Errorf("permitio saltar de DISCOVERY directo a IMPLEMENTATION")
	}
}

func TestDesdeDoneNoSeAvanza(t *testing.T) {
	s := nuevoEn(PhaseDone)
	if err := CanTransition(s, PhaseDone); err == nil {
		t.Errorf("permitio transicionar desde DONE")
	}
}

func TestMissingForListaTodoLoQueFalta(t *testing.T) {
	s := nuevoEn(PhasePlan)
	missing := MissingFor(s, PhaseImplementation)
	if len(missing) != 2 {
		t.Errorf("MissingFor devolvio %d faltantes, quiero 2: %v", len(missing), missing)
	}
}

func TestCaminoFelizCompleto(t *testing.T) {
	s := NewState("tarea")
	s.Spec = Document{Version: 1, Content: "spec", Approved: true}
	if err := CanTransition(s, PhaseDesign); err != nil {
		t.Fatalf("DISCOVERY->DESIGN: %v", err)
	}

	s.CurrentPhase = PhaseDesign
	s.Design = Document{Version: 1, Content: "design", Approved: true}
	if err := CanTransition(s, PhasePlan); err != nil {
		t.Fatalf("DESIGN->PLAN: %v", err)
	}

	s.CurrentPhase = PhasePlan
	s.Plan = Document{Version: 1, Content: "plan", Approved: true}
	if err := CanTransition(s, PhaseImplementation); err != nil {
		t.Fatalf("PLAN->IMPLEMENTATION: %v", err)
	}
}
