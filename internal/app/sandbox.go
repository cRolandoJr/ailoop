package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// prepareSandbox leaves dir as a clean detached checkout of HEAD, creating it
// the first time, and refuses to touch it when it is not one.
//
// The refusal is the point. git finds a repository by walking UP from the
// working directory, so `git reset --hard` run inside a plain directory that
// happens to sit within the project resets THE PROJECT, discarding whatever
// the person had not committed. The sandbox exists to protect the workspace;
// without this check its own setup is the most destructive path in the repo.
//
// Every failure is returned. The three commands used to run with their errors
// dropped, so a project with no repository, or a worktree that could not be
// created, skipped the whole thing in silence and looked like a feature that
// simply never fired.
func prepareSandbox(workspace, dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if out, err := git(workspace, "worktree", "add", dir, "--detach"); err != nil {
			return fmt.Errorf("could not create the sandbox worktree at %s: %w: %s", dir, err, out)
		}
		return nil
	}

	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("%s is not a git worktree, refusing to reset it: %w", dir, err)
	}
	if !samePath(top, dir) {
		return fmt.Errorf("%s is not its own worktree - git resolves it to %s - refusing to reset it",
			dir, top)
	}

	if out, err := git(dir, "reset", "--hard", "HEAD"); err != nil {
		return fmt.Errorf("could not reset the sandbox: %w: %s", err, out)
	}
	if out, err := git(dir, "clean", "-fd"); err != nil {
		return fmt.Errorf("could not clean the sandbox: %w: %s", err, out)
	}
	return nil
}

// git runs one command in dir and returns its output, trimmed.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// samePath compares two paths after resolving symlinks, because a temporary
// directory is often reached through one.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}
