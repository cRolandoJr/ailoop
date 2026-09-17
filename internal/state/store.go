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

	filePath := filepath.Join(dirPath, stateFile)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// Load reads the AIState from the target project directory
func Load(targetDir string) (*AIState, error) {
	filePath := filepath.Join(env.AILoopDir(targetDir), stateFile)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, errors.New("no active AI Loop found. Run 'ailoop start' first")
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
