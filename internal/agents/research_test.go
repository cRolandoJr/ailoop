package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
	"github.com/cRolandoJr/ailoop/internal/web"
)

func fetcherDe(hosts ...string) *web.Fetcher {
	// Sin hosts, va a cualquier destino publico, que es el modo normal.
	return &web.Fetcher{Allowed: hosts}
}

func TestElInvestigadorNoTieneAccesoLocal(t *testing.T) {
	// La propiedad central: no puede filtrar lo que nunca tuvo. Es
	// estructural, no probabilistica.
	f := &fakeLLM{replies: []string{
		pedir("fs.read", "/etc/passwd"),
		"no pude leer nada local",
	}}

	var vistos []tools.Result
	_, err := Research(context.Background(), "que es un data diode", f,
		fetcherDe("example.com"), func(r tools.Result) { vistos = append(vistos, r) })
	if err != nil {
		t.Fatal(err)
	}
	if len(vistos) != 1 {
		t.Fatalf("resultados = %d, quiero 1", len(vistos))
	}
	if vistos[0].Allowed {
		t.Error("el investigador pudo usar fs.read")
	}
	if !strings.Contains(vistos[0].Output, "REFUSED") {
		t.Errorf("no se lo rechazo: %q", vistos[0].Output)
	}
}

func TestSuPromptSoloOfreceLaRed(t *testing.T) {
	f := &fakeLLM{replies: []string{"listo"}}
	if _, err := Research(context.Background(), "una pregunta", f, fetcherDe("go.dev"), nil); err != nil {
		t.Fatal(err)
	}
	var sys string
	for _, m := range f.seen[0] {
		if m.Role == "system" {
			sys += m.Content
		}
	}
	if !strings.Contains(sys, "web.fetch") {
		t.Error("no le ofrece la red")
	}
	for _, prohibida := range []string{"fs.read", "fs.grep", "fs.glob", "cmd.run", "research.ask"} {
		if strings.Contains(sys, prohibida) {
			t.Errorf("le ofrece %s, que no le corresponde", prohibida)
		}
	}
}

func TestSoloViajaLaPreguntaNadaMas(t *testing.T) {
	// El unico dato que cruza es la pregunta. Ni el workspace, ni el estado,
	// ni el contexto del proyecto.
	f := &fakeLLM{replies: []string{"listo"}}
	if _, err := Research(context.Background(), "cual es el timeout por defecto", f,
		fetcherDe("go.dev"), nil); err != nil {
		t.Fatal(err)
	}
	var todo string
	for _, m := range f.seen[0] {
		todo += m.Content
	}
	if !strings.Contains(todo, "cual es el timeout por defecto") {
		t.Error("no le llego la pregunta")
	}
	for _, filtrado := range []string{"PROJECT_CONTEXT", "APPROVED_DECISIONS", "GIT_CONTEXT", "Spec (v"} {
		if strings.Contains(todo, filtrado) {
			t.Errorf("le llego %s, que es contexto local", filtrado)
		}
	}
}

func TestLaPreguntaTieneUnTecho(t *testing.T) {
	// Es el unico canal que queda abierto: se mantiene angosto.
	larga := strings.Repeat("a", MaxQuestionChars+1)
	_, err := Research(context.Background(), larga, &fakeLLM{}, fetcherDe("go.dev"), nil)
	if err == nil {
		t.Fatal("acepto una pregunta sin limite")
	}
	if !strings.Contains(err.Error(), "leaves this machine") {
		t.Errorf("el error no explica por que hay limite: %v", err)
	}
}

func TestSinFetcherNoHayInvestigacion(t *testing.T) {
	// Research apagada en el config significa que no hay investigador.
	if _, err := Research(context.Background(), "algo", &fakeLLM{}, nil, nil); err == nil {
		t.Error("investigo con la research apagada")
	}
}

func TestElInvestigadorBuscaDondeSea(t *testing.T) {
	// El limite es QUIEN busca, no DONDE: nunca se sabe de antemano en que
	// sitio esta la respuesta.
	f := &fakeLLM{replies: []string{"listo"}}
	abierto := &web.Fetcher{} // sin allowlist
	if _, err := Research(context.Background(), "una pregunta", f, abierto, nil); err != nil {
		t.Fatalf("no investigo con destinos abiertos: %v", err)
	}
}

func TestElPrincipalNuncaTieneLaRed(t *testing.T) {
	// En ninguna fase, con o sin agente investigador disponible.
	caps := llm.Capabilities{Vision: llm.Supported}
	research := func(context.Context, string) (string, error) { return "", nil }

	for _, ph := range state.Phases() {
		for _, r := range []func(context.Context, string) (string, error){nil, research} {
			reg := RegistryFor(ph, nil, caps, nil, r, nil)
			if reg.Allowed[tools.WebFetch] {
				t.Errorf("%s: el agente principal tiene web.fetch", ph)
			}
		}
	}
}

func TestImplementationNoInvestiga(t *testing.T) {
	// Tu 18.10 no le da "research documentation" al Implementation Agent.
	research := func(context.Context, string) (string, error) { return "", nil }
	caps := llm.Capabilities{}

	reg := RegistryFor(state.PhaseImplementation, nil, caps, nil, research, nil)
	if reg.Allowed[tools.ResearchAsk] {
		t.Error("IMPLEMENTATION puede investigar, y el contrato 18.10 no se lo da")
	}

	for _, ph := range []state.Phase{state.PhaseDiscovery, state.PhaseDesign,
		state.PhasePlan, state.PhaseVerification} {
		if !RegistryFor(ph, nil, caps, nil, research, nil).Allowed[tools.ResearchAsk] {
			t.Errorf("%s no puede investigar, y su contrato si se lo da", ph)
		}
	}
}

func TestSinInvestigadorNoSeOfreceLaDelegacion(t *testing.T) {
	reg := RegistryFor(state.PhaseDiscovery, nil, llm.Capabilities{}, nil, nil, nil)
	if reg.Allowed[tools.ResearchAsk] {
		t.Error("ofrece research.ask sin agente investigador detras")
	}
}
