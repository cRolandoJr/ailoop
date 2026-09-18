// Package ide receives what the person is looking at in their editor, so the
// agent can be told where their attention is without being asked.
package ide

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cRolandoJr/ailoop/internal/env"
)

// DefaultAddr is where editor plugins POST. It is fixed so a plugin needs no
// configuration, which also means a second ailoop cannot have it.
const DefaultAddr = "127.0.0.1:11435"

// State represents the IDE's current visual context.
type State struct {
	ActiveFile   string    `json:"active_file"`
	CursorLine   int       `json:"cursor_line"`
	SelectedText string    `json:"selected_text"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StartSyncServer listens for editor state on DefaultAddr. Editor plugins POST
// to http://127.0.0.1:11435/sync with a JSON body matching State.
//
// It returns an error instead of swallowing one: the address is fixed, so a
// second ailoop in another project cannot have it, and a server that silently
// never listens looks exactly like an editor that never sends.
func StartSyncServer(workspace string) error {
	_, err := startSyncServerOn(DefaultAddr, workspace)
	return err
}

// startSyncServerOn is StartSyncServer bound to a chosen address, which is
// what lets a test use an ephemeral port.
//
// The handler goes on a mux of its own. Registering on http.DefaultServeMux
// made this function callable exactly once per process - a second call is a
// panic, not an error - and the one-shot commands reach it again from inside
// the session.
func startSyncServerOn(addr, workspace string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("ide sync server: %w", err)
	}

	token, err := loadOrCreateToken(workspace)
	if err != nil {
		_ = ln.Close()
		return "", err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sync", syncHandler(workspace, token))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()

	return ln.Addr().String(), nil
}

func syncHandler(workspace, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// What arrives here is read into the agent's prompt, so the sender has
		// to prove it can read a file only this user can read.
		if !tokenMatches(token, r.Header.Get(TokenHeader)) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		var s State
		if err := json.Unmarshal(body, &s); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		s.UpdatedAt = time.Now()

		// Disk is the only copy on purpose. An in-memory one used to shadow it
		// from a package global with no workspace in it, so two projects open
		// at once read each other's editor state.
		//
		// The directory may not exist yet: it is created by starting a work
		// item, and the editor can be pointed here before that. The write used
		// to be discarded, and the memory copy hid that it had failed.
		stateFile := stateFilePath(workspace)
		if stateFile == "" {
			http.Error(w, "no workspace", http.StatusInternalServerError)
			return
		}
		data, err := json.Marshal(s)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(stateFile), 0o755)
		}
		if err == nil {
			err = os.WriteFile(stateFile, data, 0o600)
		}
		if err != nil {
			// Answering OK to something that was not stored would have the
			// editor believe the agent can see what it is looking at.
			http.Error(w, "could not store state", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	}
}

// GetRecentState returns this workspace's editor state when it was updated in
// the last five minutes, and nil when it was not.
//
// It reads the file every time. The read happens once per phase, so caching it
// bought nothing and cost the workspace boundary.
func GetRecentState(workspace string) *State {
	stateFile := stateFilePath(workspace)
	if stateFile == "" {
		return nil
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	if time.Since(s.UpdatedAt) >= 5*time.Minute {
		return nil
	}
	return &s
}

func stateFilePath(workspace string) string {
	dir := env.AILoopDir(workspace)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "ide_state.json")
}
