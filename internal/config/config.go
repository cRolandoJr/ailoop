// Package config holds the project's own verification commands. The project
// declares what "verified" means for it; the workflow only runs it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cRolandoJr/ailoop/internal/mcp"
	"github.com/cRolandoJr/ailoop/internal/state"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

const (
	dirName  = ".ailoop"
	fileName = "config.json"

	// DefaultTimeoutSeconds bounds a single check. A build that hangs must not
	// hang the loop.
	DefaultTimeoutSeconds = 300
)

// ErrNotFound means the project has no config yet.
var ErrNotFound = errors.New("no .ailoop/config.json found")

type Config struct {
	// Verify are the commands that decide whether the work is actually done.
	Verify []verify.Check `json:"verify"`
	// TimeoutSeconds bounds each individual check.
	TimeoutSeconds int `json:"timeout_seconds"`
	// MCP are external tool servers to connect to. Their tools become
	// capabilities of the loop without being reimplemented here.
	MCP []mcp.Server `json:"mcp,omitempty"`
	// Budget is the default ceiling for work started in this project. A run
	// can lower it with --budget.
	Budget state.Budget `json:"budget,omitempty"`
	// Web is where the research agent may go. Empty means nowhere, which is
	// the default: research is opt-in, per project.
	Web WebPolicy `json:"web,omitempty"`
	// Providers routes phases to LLM providers, so the judgment-heavy phases
	// can run on a strong model while the mechanical ones run on a cheap one
	// (docs/SPEC-ruteo-proveedor-por-fase.md). Keys: "default" plus phase
	// names in lowercase; values: "claude", "gemini", "openai". Empty means
	// the single env-selected client, exactly as before this field existed.
	Providers map[string]string `json:"providers,omitempty"`
	// LSP configures the language server protocol command for the workspace.
	// Empty means detect automatically based on files.
	LSP []string `json:"lsp,omitempty"`
}

// WebPolicy is the egress policy of this project.
type WebPolicy struct {
	// Enabled turns research on. It is explicit rather than implied by the
	// rest of the policy: an agent that can reach the internet is something
	// the project decides, not something it inherits.
	Enabled bool `json:"enabled"`
	// Allowed optionally restricts destinations. Empty - the normal case -
	// means anywhere public: you cannot know in advance where an answer is.
	Allowed []string `json:"allowed,omitempty"`
	// Blocked are destinations never reached.
	Blocked []string `json:"blocked,omitempty"`
	// MaxBytes caps one retrieved page. Zero uses the package default.
	MaxBytes int `json:"max_bytes,omitempty"`
}

func Path(targetDir string) string {
	return filepath.Join(targetDir, dirName, fileName)
}

func Load(targetDir string) (*Config, error) {
	data, err := os.ReadFile(Path(targetDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = DefaultTimeoutSeconds
	}
	// A typo here would silently route nothing: the phase you thought you
	// configured keeps the default model and no symptom names the cause.
	// Fail-closed at load, naming the broken key.
	for k := range c.Providers {
		if _, ok := providerKeys[k]; !ok {
			return nil, fmt.Errorf("providers: unknown key %q (valid: default, discovery, design, plan, implementation, verification)", k)
		}
	}
	return &c, nil
}

// providerKeys is every key Providers accepts, and the phase each one names.
// "default" maps to no phase: it replaces the env-selected client instead.
var providerKeys = map[string]state.Phase{
	"default":        "",
	"discovery":      state.PhaseDiscovery,
	"design":         state.PhaseDesign,
	"plan":           state.PhasePlan,
	"implementation": state.PhaseImplementation,
	"verification":   state.PhaseVerification,
}

// PhaseProviders resolves the Providers block into a phase→provider map plus
// the default provider name ("" when unset). Keys were validated at Load, so
// this cannot fail.
func (c *Config) PhaseProviders() (map[state.Phase]string, string) {
	byPhase := map[state.Phase]string{}
	def := ""
	for k, v := range c.Providers {
		if k == "default" {
			def = v
			continue
		}
		if ph, ok := providerKeys[k]; ok {
			byPhase[ph] = v
		}
	}
	return byPhase, def
}

func Save(targetDir string, c *Config) error {
	if err := os.MkdirAll(filepath.Join(targetDir, dirName), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(targetDir), data, 0644)
}

// Detect builds a starting config from what the project actually contains.
// It only knows the stacks we have a real project for today; anything else
// gets an empty list the user fills in, never a guessed command.
func Detect(targetDir string) *Config {
	c := &Config{
		TimeoutSeconds: DefaultTimeoutSeconds,
		// Research on by default, and written into the file so it is visible
		// and can be turned off. A setting nobody can see is not a choice.
		Web: WebPolicy{Enabled: true},
	}

	if exists(filepath.Join(targetDir, "go.mod")) {
		c.Verify = []verify.Check{
			{Name: "build", Cmd: "go build ./..."},
			{Name: "vet", Cmd: "go vet ./..."},
			{Name: "test", Cmd: "go test ./... -count=1"},
		}
	}

	return c
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
