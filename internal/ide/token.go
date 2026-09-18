package ide

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/env"
)

// TokenHeader is where the editor plugin puts the shared secret.
//
// A custom header is not a simple request, so a browser has to ask permission
// with a preflight before sending one - and this server answers no. That is
// belt and braces: the secret alone already stops a page, because a page
// cannot read a file.
const TokenHeader = "X-AILoop-Token"

// TokenPath is the file the editor plugin reads the secret from.
func TokenPath(workspace string) string {
	return filepath.Join(env.RootDir(workspace), "sync_token")
}

// loadOrCreateToken returns this workspace's secret, minting one the first
// time. It is kept rather than regenerated so a plugin can read it once.
//
// The failure is deliberately loud: with no secret there is no safe way to
// serve, so the server does not start. The channel writes into the agent's
// prompt, and an open one is a way in for anything running on this machine -
// including a web page, which can POST to localhost even though it cannot
// read the answer.
func loadOrCreateToken(workspace string) (string, error) {
	path := TokenPath(workspace)

	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("ide sync token: %w", err)
	}
	tok := hex.EncodeToString(raw)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("ide sync token: %w", err)
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("ide sync token: %w", err)
	}
	return tok, nil
}

// tokenMatches compares in constant time, so a wrong guess cannot be refined
// by how long the answer took.
func tokenMatches(want, got string) bool {
	return subtle.ConstantTimeCompare([]byte(want), []byte(strings.TrimSpace(got))) == 1
}
