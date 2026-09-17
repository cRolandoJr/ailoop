package patch

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUnread means a patch targets a file the agent never read.
var ErrUnread = errors.New("patch targets a file the agent never read")

// Seen is the set of workspace files an agent actually looked at.
//
// An agent that emits a SEARCH/REPLACE block for a file it never opened has
// invented the search text. Today that usually fails with "search block not
// found", but it fails LATE - after the proposal was made, after the human
// approved it, and after the phase moved. Worse, on repetitive code (imports,
// boilerplate, a common error string) an invented block can MATCH, and then
// the patch lands somewhere nobody chose.
//
// Requiring the file to have been read turns a silent hallucination into an
// early refusal with a clear message.
//
// What this does NOT do: detect that the file changed after it was read. That
// is what the exact search block is for, and it already covers it.
type Seen map[string]bool

// NewSeen builds a set from the paths an agent read.
func NewSeen(paths ...string) Seen {
	s := Seen{}
	for _, p := range paths {
		s.Add(p)
	}
	return s
}

// Add records a file as read. Paths are normalised so that "./a/b.go" and
// "a/b.go" are the same file.
func (s Seen) Add(path string) {
	if s == nil {
		return
	}
	if clean, err := SafeRelPath(".", path); err == nil {
		s[clean] = true
	} else {
		s[path] = true
	}
}

// Has reports whether the file was read.
func (s Seen) Has(path string) bool {
	return s != nil && s[path]
}

// Paths lists what was read, sorted, for messages and for persisting.
func (s Seen) Paths() []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// unreadError explains the refusal in terms the agent can act on.
func unreadError(path string, seen Seen) error {
	read := seen.Paths()
	if len(read) == 0 {
		return fmt.Errorf("%w: %s. Read it with fs.read before proposing a change to it",
			ErrUnread, path)
	}
	return fmt.Errorf("%w: %s. You read: %s. Read %s with fs.read before proposing a change to it",
		ErrUnread, path, strings.Join(read, ", "), path)
}
