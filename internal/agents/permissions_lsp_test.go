package agents

import (
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/lsp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// stubLSP stands in for a language server that did start.
type stubLSP struct{ lspIface }

// A *lsp.Client that failed to start is nil, but a nil pointer stored in an
// interface is not a nil interface: the comparison is false and the
// capability would be offered with nothing behind it. The composition root
// passes exactly this type, so the guard has to survive it.
func TestRegistryForRefusesLSPWhenTheClientIsATypedNil(t *testing.T) {
	var client *lsp.Client // the server never started

	reg := RegistryFor(state.PhaseDiscovery, nil, llm.Capabilities{}, nil, nil, client)

	for _, c := range []tools.Capability{tools.LSPDefinition, tools.LSPReferences} {
		if reg.Allowed[c] {
			t.Errorf("%s was granted with no language server behind it", c)
		}
	}
	if reg.LSP != nil {
		t.Error("the registry kept a language server that is not there")
	}
}

// With a server actually present the capability is granted, so the guard
// above is refusing the right thing and not simply refusing everything.
func TestRegistryForGrantsLSPWhenAClientIsPresent(t *testing.T) {
	reg := RegistryFor(state.PhaseDiscovery, nil, llm.Capabilities{}, nil, nil, &stubLSP{})

	for _, c := range []tools.Capability{tools.LSPDefinition, tools.LSPReferences} {
		if !reg.Allowed[c] {
			t.Errorf("%s was refused although a language server is connected", c)
		}
	}
}

// Verification reads and runs the project's checks; it does not need symbol
// navigation, and the phase map is where that choice is recorded.
func TestVerificationHasNoLSP(t *testing.T) {
	reg := RegistryFor(state.PhaseVerification, nil, llm.Capabilities{}, nil, nil, &stubLSP{})
	if reg.Allowed[tools.LSPDefinition] || reg.Allowed[tools.LSPReferences] {
		t.Error("verification was granted symbol navigation")
	}
}
