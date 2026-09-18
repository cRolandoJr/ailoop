package agents

import (
	"context"
	"reflect"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/tools"
)

// phaseCapabilities is the workflow's permission policy: what each agent may
// do, by phase. It is deliberately narrow.
//
// AI_LOOP 18.16: a capability being available never grants permission to use
// it. This map is where that distinction lives.
var phaseCapabilities = map[state.Phase][]tools.Capability{
	// Reading the ground is what stops an agent from inventing it.
	state.PhaseDiscovery:      {tools.ResearchAsk, tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.FSReadImage, tools.LSPDefinition, tools.LSPReferences},
	state.PhaseDesign:         {tools.ResearchAsk, tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.FSReadImage, tools.LSPDefinition, tools.LSPReferences},
	state.PhasePlan:           {tools.ResearchAsk, tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.LSPDefinition, tools.LSPReferences},
	state.PhaseImplementation: {tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.FSReadImage, tools.LSPDefinition, tools.LSPReferences},
	// Only the verifier may run the project's checks, and only the ones the
	// project declared. An implementer that can run and fix its own tests is
	// its own verifier, which AI_LOOP 18.2 forbids.
	state.PhaseVerification: {tools.ResearchAsk, tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.CmdRun},
}

// lspIface is the slice of the language server the registry needs. It is an
// interface so a test can stand one in without a subprocess.
type lspIface interface {
	Definition(ctx context.Context, path string, line, char int) (string, error)
	References(ctx context.Context, path string, line, char int) (string, error)
}

// RegistryFor builds the permission set for a phase, intersected with what
// the selected model and this environment can actually do.
//
// This is the chain of AI_LOOP 18.16, in code:
//
//	provider -> capability discovery -> policy -> agent permissions
//
// Two independent gates, and a capability needs both: the workflow has to
// allow it for this phase, and the thing behind it has to actually be there.
// Vision is where that matters for the model - offering fs.read_image to a
// model that cannot see would produce an agent confidently describing an
// image it never got - and the language server is the same shape.
//
// declaredCmds comes from the project's verification config: the agent cannot
// run a command nobody declared, so there is no arbitrary shell in this path.
func RegistryFor(phase state.Phase, declaredCmds map[string]string, caps llm.Capabilities,
	pool *mcp.Pool, research func(context.Context, string) (string, error), lspClient lspIface) *tools.Registry {
	allowed := map[tools.Capability]bool{}
	for _, c := range phaseCapabilities[phase] {
		if c == tools.FSReadImage && !caps.Vision.OK() {
			// Unknown counts as not available: fail-closed.
			continue
		}
		allowed[c] = true
	}
	// MCP tools are only offered when a server actually answered. Listing a
	// capability whose server failed to start would have the agent planning
	// around a tool that is not there.
	if pool == nil || pool.Empty() {
		delete(allowed, tools.MCPDescribe)
		delete(allowed, tools.MCPCall)
	}

	// Research is delegation, not network access: the primary agent never gets
	// web.fetch, in any phase. AI_LOOP 18.15.7.
	if research == nil {
		delete(allowed, tools.ResearchAsk)
	}
	delete(allowed, tools.WebFetch)

	// Same rule for symbol navigation, with one extra trap: a *lsp.Client that
	// never started is a nil POINTER, and a nil pointer inside an interface is
	// not a nil interface. Comparing against nil alone answers false and hands
	// the agent two capabilities with nothing behind them.
	if isAbsent(lspClient) {
		lspClient = nil
		delete(allowed, tools.LSPDefinition)
		delete(allowed, tools.LSPReferences)
	}

	return &tools.Registry{
		Allowed:      allowed,
		RunnableCmds: declaredCmds,
		MCP:          pool,
		Research:     research,
		LSP:          lspClient,
	}
}

// isAbsent reports whether an interface holds nothing, including the case of
// a nil pointer stored in it. Go has no other way to ask.
func isAbsent(v lspIface) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}
