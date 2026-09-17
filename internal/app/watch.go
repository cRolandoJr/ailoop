package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/state"
)

// debounce waits out the editor writing several files in a burst.
const debounce = time.Second

// Watch monitors the workspace for file changes using lightweight polling.
// When a change is detected, it runs verification. If verification fails,
// it triggers the AI to fix it by calling Advance.
func (l *Loop) Watch(ctx context.Context, opts AdvanceOptions) error {
	lastMod := time.Now()

	opts.progress("watch", "Starting autonomous TDD watcher. Waiting for file changes...")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			// Poll files
			changed := false
			err := filepath.WalkDir(l.workspace, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				// Skip hidden dirs like .git and .ailoop
				if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if !d.IsDir() {
					info, err := d.Info()
					if err == nil && info.ModTime().After(lastMod) {
						changed = true
						lastMod = time.Now()
						return filepath.SkipDir // Stop walking, we found a change
					}
				}
				return nil
			})

			if err != nil {
				continue
			}

			if changed {
				if err := l.OnChange(ctx, opts); err != nil {
					// Over budget stops the watcher. Retrying every two
					// seconds against a ceiling that will not move is how an
					// unattended loop burns a whole budget on one error.
					if errors.Is(err, state.ErrOverBudget) {
						opts.progress("budget", err.Error())
						return err
					}
					opts.progress("watch-error", err.Error())
				}
				lastMod = time.Now() // after the AI may have touched files
			}
		}
	}
}

// OnChange is what the watcher does when the workspace changed: verify, and
// only wake the agent if the project's own checks say something is broken.
//
// Split out of the polling loop so it can be exercised without waiting for a
// timer - and because detecting a change and deciding what to do about it are
// two different jobs.
func (l *Loop) OnChange(ctx context.Context, opts AdvanceOptions) error {
	opts.progress("watch-triggered", "File change detected. Running verification...")
	time.Sleep(debounce)

	report, err := l.Verify(ctx)
	if err != nil {
		// Not running is not passing. Reading a failed verification as a green
		// one is the vacuous pass this project exists to avoid, and here it
		// would happen unattended.
		return fmt.Errorf("could not verify: %w", err)
	}
	opts.progress("watch-verify", "Verification complete.")

	if report.Passed {
		opts.progress("watch-pass", "Checks passed. Nothing for the agent to do.")
		return nil
	}

	opts.progress("watch-fail", "Checks failed. Waking the agent to fix it...")

	s, err := l.load()
	if err != nil {
		return err
	}
	// Only a loop already at the implementation end of the line is resumed.
	// Moving it there from anywhere else would write a phase the guards never
	// authorised, and leave the state claiming approvals that do not exist.
	if s.CurrentPhase != state.PhaseImplementation && s.CurrentPhase != state.PhaseVerification {
		opts.progress("watch-skip", fmt.Sprintf(
			"loop is in %s, not implementing. Run 'ailoop next' to advance it.", s.CurrentPhase))
		return nil
	}

	if _, err := l.Advance(ctx, opts); err != nil {
		return err
	}
	opts.progress("watch", "Resuming watch mode...")
	return nil
}
