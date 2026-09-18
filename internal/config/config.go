// Package config holds the project's own verification commands. The project
// declares what "verified" means for it; the workflow only runs it.
package config

import (
	"encoding/json"
	"errors"
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
	return &c, nil
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
