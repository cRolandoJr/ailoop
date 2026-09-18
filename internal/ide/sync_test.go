package ide

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// Starting twice used to panic: the handler was registered on the global
// http.DefaultServeMux, and a second registration of the same pattern is
// fatal. It happened for real - "/verify" inside the session reaches the
// composition root a second time and took the whole process down.
func TestStartingTwiceDoesNotPanic(t *testing.T) {
	if _, err := startSyncServerOn("127.0.0.1:0", t.TempDir()); err != nil {
		t.Fatalf("first server: %v", err)
	}
	if _, err := startSyncServerOn("127.0.0.1:0", t.TempDir()); err != nil {
		t.Fatalf("second server: %v", err)
	}
}

// A port already taken - another ailoop in another project - must be an
// error the caller can report, not a server that silently never listens.
func TestAPortAlreadyTakenIsReported(t *testing.T) {
	addr, err := startSyncServerOn("127.0.0.1:0", t.TempDir())
	if err != nil {
		t.Fatalf("first server: %v", err)
	}

	if _, err := startSyncServerOn(addr, t.TempDir()); err == nil {
		t.Fatal("binding the same address twice reported no error")
	}
}

// The positive control: "it did not panic" would also be true of a server
// that serves nothing. This one has to actually take a POST and hand the
// state back.
func TestPostedStateComesBackFromGetRecentState(t *testing.T) {
	ws := t.TempDir()
	addr, err := startSyncServerOn("127.0.0.1:0", ws)
	if err != nil {
		t.Fatalf("server: %v", err)
	}

	tok, err := os.ReadFile(TokenPath(ws))
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	body := `{"active_file":"internal/app/advance.go","cursor_line":171,"selected_text":"s.AutoRetries"}`
	resp := postSync(t, addr, strings.TrimSpace(string(tok)), body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST returned %s", resp.Status)
	}

	got := GetRecentState(ws)
	if got == nil {
		t.Fatal("the posted state did not come back")
	}
	if got.ActiveFile != "internal/app/advance.go" || got.CursorLine != 171 {
		t.Fatalf("got %+v, want the file and line that were posted", got)
	}
}
