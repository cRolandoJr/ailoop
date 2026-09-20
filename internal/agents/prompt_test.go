package agents

import (
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/state"
)

// El defecto medido el 17-sep: applyTo CREA un archivo cuando el bloque llega
// con SEARCH vacio, pero el prompt de IMPLEMENTATION solo decia "to modify
// files". El modelo probo fs.create, fs.write, y termino pidiendo crear el
// archivo a mano — asi que Tier 0 y el sandbox nunca dispararon. El prompt y
// el parser son un contrato: lo que uno hace, el otro lo tiene que decir.
func TestElPromptDeImplementacionEnsenaACrearArchivos(t *testing.T) {
	p := getSystemPromptForPhase(state.PhaseImplementation)

	frase := strings.ToLower(p)
	if !strings.Contains(frase, "create a new file") {
		t.Fatal("el prompt no dice como se crea un archivo nuevo")
	}
	// La mecanica exacta, no una alusion: SEARCH vacio. Sin esta mitad el
	// modelo sabe QUE se puede y sigue sin saber COMO.
	if !strings.Contains(frase, "search") || !strings.Contains(frase, "empty") {
		t.Fatal("el prompt no dice que el bloque de busqueda va VACIO para crear")
	}
}
