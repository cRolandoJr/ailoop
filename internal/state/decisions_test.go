package state

import (
	"strings"
	"testing"
)

func TestSupersedeConservaLaDecisionVieja(t *testing.T) {
	// Una decision revisada no se borra: se marca. Perder la version anterior
	// es perder por que se decidio distinto.
	var r DecisionRecord
	vieja := r.Add("usar JSON", "cero dependencias", nil, PhaseDesign)

	nueva, err := r.Supersede(vieja.ID, "usar YAML", "el usuario lo edita a mano", nil, PhaseDesign)
	if err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	if len(r.Decisions) != 2 {
		t.Fatalf("Decisions = %d, quiero 2 (la vieja se conserva)", len(r.Decisions))
	}
	if r.Decisions[0].SupersededBy != nueva.ID {
		t.Errorf("la vieja no quedo marcada como superseded: %+v", r.Decisions[0])
	}
	if nueva.Version != 2 {
		t.Errorf("la nueva tiene version %d, quiero 2", nueva.Version)
	}
	if act := r.Active(); len(act) != 1 || act[0].ID != nueva.ID {
		t.Errorf("Active() = %+v, quiero solo la nueva", act)
	}
}

func TestNoSePuedeSupersederDosVeces(t *testing.T) {
	var r DecisionRecord
	d := r.Add("A", "", nil, PhaseDiscovery)
	if _, err := r.Supersede(d.ID, "B", "", nil, PhaseDiscovery); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Supersede(d.ID, "C", "", nil, PhaseDiscovery); err == nil {
		t.Errorf("permitio superseder una decision ya reemplazada")
	}
}

func TestBriefSoloLlevaLasActivas(t *testing.T) {
	var r DecisionRecord
	d := r.Add("el test es sintetico", "determinismo", []string{"E-03"}, PhaseDiscovery)
	if _, err := r.Supersede(d.ID, "el test usa el corpus real", "fidelidad", nil, PhaseDesign); err != nil {
		t.Fatal(err)
	}

	b := r.Brief()
	if strings.Contains(b, "el test es sintetico") {
		t.Errorf("Brief incluyo una decision superseded:\n%s", b)
	}
	if !strings.Contains(b, "el test usa el corpus real") {
		t.Errorf("Brief no incluyo la decision activa:\n%s", b)
	}
}

func TestBriefVacioCuandoNoHayDecisiones(t *testing.T) {
	var r DecisionRecord
	if r.Brief() != "" {
		t.Errorf("Brief devolvio texto sin decisiones: %q", r.Brief())
	}
}
