package state

import (
	"testing"
	"time"
)

func TestReviseNoPierdeElContenidoAnterior(t *testing.T) {
	// El bug que esto arregla: rechazar una propuesta borraba la anterior y
	// solo quedaba un numero de version contando perdidas.
	d := &Document{Version: 1, Content: "primera propuesta", Approved: true}

	d.Revise("segunda propuesta", "no contemplaba el caso vacio", time.Now())

	if d.Content != "segunda propuesta" {
		t.Errorf("Content = %q, quiero la nueva", d.Content)
	}
	if d.Version != 2 {
		t.Errorf("Version = %d, quiero 2", d.Version)
	}
	if len(d.History) != 1 {
		t.Fatalf("History tiene %d revisiones, quiero 1", len(d.History))
	}

	vieja, ok := d.RevisionAt(1)
	if !ok {
		t.Fatalf("no se puede recuperar la version 1")
	}
	if vieja.Content != "primera propuesta" {
		t.Errorf("la v1 recuperada = %q, quiero el texto original", vieja.Content)
	}
	if vieja.RejectedBecause != "no contemplaba el caso vacio" {
		t.Errorf("no se guardo el motivo del rechazo: %q", vieja.RejectedBecause)
	}
}

func TestUnaRevisionNuevaNoHeredaLaAprobacion(t *testing.T) {
	// La aprobacion es del texto exacto que se aprobo. Si el texto cambia,
	// la aprobacion caduca: si no, un guard dejaria pasar contenido que
	// ningun humano vio.
	d := &Document{Version: 1, Content: "aprobado", Approved: true}
	d.Revise("texto distinto", "cambios pedidos", time.Now())

	if d.Approved {
		t.Errorf("Approved = true tras revisar: la aprobacion se heredo indebidamente")
	}
}

func TestVariasRevisionesSonTodasRecuperables(t *testing.T) {
	d := &Document{Version: 1, Content: "v1"}
	d.Revise("v2", "motivo A", time.Now())
	d.Revise("v3", "motivo B", time.Now())

	if len(d.History) != 2 {
		t.Fatalf("History = %d, quiero 2", len(d.History))
	}
	for _, caso := range []struct {
		v      int
		quiero string
	}{{1, "v1"}, {2, "v2"}} {
		r, ok := d.RevisionAt(caso.v)
		if !ok || r.Content != caso.quiero {
			t.Errorf("RevisionAt(%d) = %q/%v, quiero %q", caso.v, r.Content, ok, caso.quiero)
		}
	}
}

func TestReviseDesdeVacioNoArchivaUnFantasma(t *testing.T) {
	d := &Document{Version: 1}
	d.Revise("primera", "", time.Now())
	if len(d.History) != 0 {
		t.Errorf("archivo una revision vacia: %+v", d.History)
	}
}

func TestReopenCurrentPhaseRetiraLaAprobacion(t *testing.T) {
	// Volver atras no puede dejar aprobado algo que nadie volvio a mirar.
	s := NewState("tarea")
	s.CurrentPhase = PhaseDesign
	s.Design = Document{Version: 2, Content: "diseno aprobado", Approved: true}

	s.ReopenCurrentPhase(time.Now())

	if s.Design.Approved {
		t.Error("quedo aprobado despues de reabrir la fase")
	}
	if s.Design.Content != "" {
		t.Errorf("no vacio el contenido: %q", s.Design.Content)
	}
	if len(s.Design.History) != 1 || s.Design.History[0].Content != "diseno aprobado" {
		t.Errorf("no archivo lo que habia: %+v", s.Design.History)
	}
}

func TestRecordApprovalEscribeEnElArtefactoDeLaFase(t *testing.T) {
	s := NewState("tarea")
	s.CurrentPhase = PhasePlan
	s.RecordApproval("el plan")

	if s.Plan.Content != "el plan" || !s.Plan.Approved {
		t.Errorf("Plan = %+v", s.Plan)
	}
	if s.Spec.Content != "" || s.Design.Content != "" {
		t.Error("escribio en el artefacto de otra fase")
	}
}

func TestRejectCurrentProposalArchivaEnLaFaseCorrecta(t *testing.T) {
	s := NewState("tarea")
	s.CurrentPhase = PhaseDesign
	s.RejectCurrentProposal("propuesta mala", "no contempla X", time.Now())

	if len(s.Design.History) != 1 {
		t.Fatalf("Design.History = %d, quiero 1", len(s.Design.History))
	}
	if len(s.Spec.History) != 0 {
		t.Error("archivo en el artefacto equivocado")
	}
}
