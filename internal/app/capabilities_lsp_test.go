package app

import (
	"context"
	"os/exec"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/lsp"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

func concedida(r *CapabilitiesReport, c tools.Capability) bool {
	for _, g := range r.ByPhase {
		for _, got := range g.Granted {
			if got == c {
				return true
			}
		}
	}
	return false
}

// El informe describe lo que el loop concede. Si se arma sin el servidor de
// lenguaje, no puede decir la verdad sobre el: dice que no esta concedido
// aunque lo este.
func TestCapabilitiesInformaElLSPCuandoElLoopTieneUno(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("no hay 'cat' para hacer de servidor mudo")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 'cat' alcanza: Capabilities no habla con el servidor, solo pregunta si
	// hay uno.
	cliente, err := lsp.Start(ctx, "cat")
	if err != nil {
		t.Fatal(err)
	}

	l := NewLoop(t.TempDir(), &modeloScript{}, nil, &config.Config{}, cliente)

	r, err := l.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	if !concedida(r, tools.LSPDefinition) {
		t.Error("el informe no concede lsp.definition habiendo servidor")
	}
}

// Y sin servidor no lo concede, para que el informe no mienta al reves.
func TestCapabilitiesNoInformaElLSPSinServidor(t *testing.T) {
	l := NewLoop(t.TempDir(), &modeloScript{}, nil, &config.Config{}, nil)

	r, err := l.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	if concedida(r, tools.LSPDefinition) {
		t.Error("el informe concede lsp.definition sin servidor detras")
	}
}
