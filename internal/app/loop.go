// Package app holds the use cases of the loop: what the system does when the
// user asks for something.
//
// Everything here returns data and never prints. The rule is not cosmetic -
// it is what makes these functions testable at all. A use case that formats
// its own output can only be verified by reading a terminal; one that returns
// a value can be verified by a test.
//
// The presentation layer lives in internal/cli and is deliberately dumb: so
// dumb it needs no tests of its own.
package app

import (
	"errors"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/web"
)

// ErrNoClient means the use case needs a model and none was provided.
var ErrNoClient = errors.New("no LLM client configured")

// Loop is one work item being acted on. Its dependencies are injected: the
// composition root that builds them lives in main, not here, so a test can
// hand it a fake without touching the CLI.
type Loop struct {
	workspace string
	client    llm.Client
	pool      *mcp.Pool
	cfg       *config.Config
}

// NewLoop builds a loop over a workspace. client, pool and cfg may be nil for
// use cases that do not need them; the ones that do return a clear error.
func NewLoop(workspace string, client llm.Client, pool *mcp.Pool, cfg *config.Config) *Loop {
	return &Loop{workspace: workspace, client: client, pool: pool, cfg: cfg}
}

// Workspace is the directory this loop operates on.
func (l *Loop) Workspace() string { return l.workspace }

// load reads the current state, or reports that no loop was started.
func (l *Loop) load() (*state.AIState, error) {
	return state.Load(l.workspace)
}

// save persists the state.
func (l *Loop) save(s *state.AIState) error {
	return state.Save(l.workspace, s)
}

// declaredCommands is the set of verification commands the project declared,
// which is the only thing an agent is ever allowed to run.
func (l *Loop) declaredCommands() map[string]string {
	cmds := map[string]string{}
	if l.cfg == nil {
		return cmds
	}
	for _, chk := range l.cfg.Verify {
		cmds[chk.Name] = chk.Cmd
	}
	return cmds
}

// web returns the fetcher for the research agent, or nil when the project
// turned research off.
func (l *Loop) web() *web.Fetcher {
	if l.cfg == nil || !l.cfg.Web.Enabled {
		return nil
	}
	return &web.Fetcher{
		Allowed:  l.cfg.Web.Allowed,
		Blocked:  l.cfg.Web.Blocked,
		MaxBytes: l.cfg.Web.MaxBytes,
	}
}
