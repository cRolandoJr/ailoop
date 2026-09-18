package cli

import (
	"errors"

	"github.com/cRolandoJr/ailoop/internal/state"
)

// ShellHint is what to add to an error when it is being reported to someone
// standing at a shell prompt. It is empty when there is nothing useful to
// add, and it lives here rather than in the error because the same error
// reaches the session, where this advice would be wrong.
func ShellHint(err error) string {
	if errors.Is(err, state.ErrNoActiveLoop) {
		return `Run: ailoop start "what you want to work on"`
	}
	return ""
}
