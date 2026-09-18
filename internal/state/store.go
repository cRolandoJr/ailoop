package state

import (
	"encoding/json"
	"errors"
	"github.com/cRolandoJr/ailoop/internal/env"
	"os"
	"path/filepath"
)

const stateFile = "state.json"

// Save writes the AIState to the target project directory
func Save(targetDir string, s *AIState) error {
	dirPath := env.AILoopDir(targetDir)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// Write beside the destination and rename over it.
	//
	// os.WriteFile truncates first, so between the truncate and the write the
	// work item does not exist: a concurrent reader gets "unexpected end of
	// JSON input", and a process that dies in that window loses it outright.
	// rename(2) within one filesystem is atomic - a reader sees the old state
	// or the new one, never half of either.
	filePath := filepath.Join(dirPath, stateFile)
	tmp, err := os.CreateTemp(dirPath, stateFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // does nothing once the rename has moved it

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp makes it 0600; the state file has always been readable.
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filePath)
}

// ErrNoActiveLoop means this directory has no work item.
//
// It carries no advice on purpose. What to do about it depends on where the
// person is standing: from the shell it is a command to run, and inside the
// session it is a line to type. The domain does not know which, so it says
// only what is true.
var ErrNoActiveLoop = errors.New("no active AI Loop here")

// Load reads the AIState from the target project directory
func Load(targetDir string) (*AIState, error) {
	filePath := filepath.Join(env.AILoopDir(targetDir), stateFile)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, ErrNoActiveLoop
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var s AIState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	return &s, nil
}
