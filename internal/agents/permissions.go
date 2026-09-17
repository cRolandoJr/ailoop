package agents

import (
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
	state.PhaseDiscovery:      {tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.FSReadImage},
	state.PhaseDesign:         {tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.FSReadImage},
	state.PhasePlan:           {tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep},
	state.PhaseImplementation: {tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.FSReadImage},
	// Only the verifier may run the project's checks, and only the ones the
	// project declared. An implementer that can run and fix its own tests is
	// its own verifier, which AI_LOOP 18.2 forbids.
	state.PhaseVerification: {tools.MCPDescribe, tools.MCPCall, tools.FSRead, tools.FSList, tools.FSGlob, tools.FSGrep, tools.CmdRun},
}

// RegistryFor builds the permission set for a phase, intersected with what
// the selected model can actually do.
//
// This is the chain of AI_LOOP 18.16, in code:
//
//	provider -> capability discovery -> policy -> agent permissions
//
// Two independent gates, and a capability needs both: the workflow has to
// allow it for this phase, and the model has to actually have it. Vision is
// where that matters today - offering fs.read_image to a model that cannot
// see would produce an agent confidently describing an image it never got.
//
// declaredCmds comes from the project's verification config: the agent cannot
// run a command nobody declared, so there is no arbitrary shell in this path.
func RegistryFor(phase state.Phase, declaredCmds map[string]string, caps llm.Capabilities, pool *mcp.Pool) *tools.Registry {
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

	return &tools.Registry{
		Allowed:      allowed,
		RunnableCmds: declaredCmds,
		MCP:          pool,
	}
}
