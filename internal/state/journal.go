package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

// journalFile is where turns accumulate, next to the rest of the loop's
// per-workspace state.
const journalFile = "turns.jsonl"

// A Turn is one agent run: what it cost, what it touched, what it ran.
//
// Ledger answers "what has this work item cost", aggregated by phase and
// carried inside the state file. This answers a different question — "what did
// THAT agent actually do" — and the distinction is not academic. A run of the
// circuit left probe files inside a live repository, and finding that out took
// an afternoon of reconstructing it from transcripts, twice with a broken
// instrument. With the turn recorded it is one read.
//
// Provider and Model are here because a regression has two possible causes and
// they look identical in the output: the prompt changed, or the weights did.
// Without the model on the record the two are indistinguishable.
//
// FilesRead and CommandsRun are what the agent asked the tool registry for, so
// they are exactly that and are named for it. Writes do not pass through the
// registry — they arrive as patches and internal/patch already keeps a backup
// per file — so folding those in waits for a reason to need them in the same
// place, rather than a field here that nothing fills.
type Turn struct {
	Time            time.Time `json:"time"`
	Phase           string    `json:"phase"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	Rounds          int       `json:"rounds"`
	Usage           llm.Usage `json:"usage"`
	FilesRead       []string  `json:"files_read,omitempty"`
	CommandsRun     []string  `json:"commands_run,omitempty"`
	DurationSeconds float64   `json:"duration_s"`
	Err             string    `json:"error,omitempty"`
}

// JournalPath is where AppendTurn writes for this workspace.
func JournalPath(workspace string) string {
	return filepath.Join(workspace, ".ailoop", journalFile)
}

// AppendTurn records one turn and reports nothing.
//
// The silence is deliberate. Journalling must never be the reason a phase
// fails: a lost line costs a gap in the log, a returned error costs the work.
// Every failure path here ends in a return for that reason.
func AppendTurn(workspace string, t Turn) {
	if workspace == "" {
		return
	}
	if t.Time.IsZero() {
		t.Time = time.Now()
	}
	path := JournalPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	line, err := json.Marshal(t)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// ReadTurns returns the turns newer than cutoff; a zero cutoff returns all.
//
// A malformed line is skipped, not fatal. The file is appended to by a process
// that can be killed mid-write, so a truncated last line is an expected state
// of a healthy journal, and refusing to read the other 200 turns because of it
// would make the log useless exactly when something went wrong.
func ReadTurns(workspace string, cutoff time.Time) ([]Turn, error) {
	data, err := os.ReadFile(JournalPath(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var turns []Turn
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var t Turn
		if json.Unmarshal([]byte(line), &t) != nil {
			continue
		}
		if !cutoff.IsZero() && t.Time.Before(cutoff) {
			continue
		}
		turns = append(turns, t)
	}
	return turns, nil
}
