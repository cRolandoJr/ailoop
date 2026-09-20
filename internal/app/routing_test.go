package app

import (
	"context"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// modeloConCaps es un modeloScript cuyo Describe es configurable: lo unico
// que este archivo necesita distinguir es QUE cliente atendio y QUE capacidades
// declaro.
type modeloConCaps struct {
	modeloScript
	caps llm.Capabilities
}

func (m *modeloConCaps) Describe() llm.Capabilities { return m.caps }

// El ruteo por fase existe para que el juicio corra en un modelo fuerte y lo
// mecanico en uno barato (SPEC-ruteo-proveedor-por-fase D-5): la fase listada
// usa SU cliente y el default no la atiende.
func TestLaFaseRuteadaUsaSuClienteYNoElDefault(t *testing.T) {
	porDefecto := &modeloScript{}
	deDiscovery := &modeloScript{}

	s := state.NewState("tarea")
	l := loopParaAvanzar(t, s, nil, porDefecto)
	l.RouteClients(map[state.Phase]llm.Client{state.PhaseDiscovery: deDiscovery})

	if _, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre}); err != nil {
		t.Fatal(err)
	}
	if deDiscovery.llamadas == 0 {
		t.Error("la fase ruteada no uso su cliente")
	}
	if porDefecto.llamadas != 0 {
		t.Errorf("el default atendio una fase ruteada (%d llamadas)", porDefecto.llamadas)
	}
}

func TestUnaFaseNoListadaUsaElDefault(t *testing.T) {
	porDefecto := &modeloScript{}
	deDesign := &modeloScript{}

	s := state.NewState("tarea") // arranca en DISCOVERY, que NO esta ruteada
	l := loopParaAvanzar(t, s, nil, porDefecto)
	l.RouteClients(map[state.Phase]llm.Client{state.PhaseDesign: deDesign})

	if _, err := l.Advance(context.Background(), AdvanceOptions{Review: apruebaSiempre}); err != nil {
		t.Fatal(err)
	}
	if porDefecto.llamadas == 0 {
		t.Error("la fase no listada no uso el default")
	}
	if deDesign.llamadas != 0 {
		t.Errorf("el cliente de DESIGN atendio DISCOVERY (%d llamadas)", deDesign.llamadas)
	}
}

// Los grants de una fase se computan con las capacidades del cliente RUTEADO:
// computarlos con el default MIENTE — ofreceria fs.read_image a una fase cuyo
// modelo no ve, o se lo negaria a una cuyo modelo si (SPEC D-6).
func TestCapabilitiesReportaProveedorYCapsDeLaFaseRuteada(t *testing.T) {
	ve := &modeloConCaps{caps: llm.Capabilities{Provider: "veo", Model: "m", Vision: llm.Supported}}

	s := state.NewState("tarea")
	l := loopParaAvanzar(t, s, nil, &modeloScript{}) // default: vision Unknown
	l.RouteClients(map[state.Phase]llm.Client{state.PhaseDiscovery: ve})

	r, err := l.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	tiene := func(g PhaseGrants, c tools.Capability) bool {
		for _, x := range g.Granted {
			if x == c {
				return true
			}
		}
		return false
	}
	for _, g := range r.ByPhase {
		switch g.Phase {
		case state.PhaseDiscovery:
			if g.Provider != "veo" {
				t.Errorf("DISCOVERY reporta proveedor %q, quiero veo", g.Provider)
			}
			if !tiene(g, tools.FSReadImage) {
				t.Error("DISCOVERY ruteada a un modelo que VE no recibio fs.read_image")
			}
		case state.PhaseDesign:
			if g.Provider != "fake" {
				t.Errorf("DESIGN reporta proveedor %q, quiero el default fake", g.Provider)
			}
			if tiene(g, tools.FSReadImage) {
				t.Error("DESIGN con el default ciego recibio fs.read_image")
			}
		}
	}
}
