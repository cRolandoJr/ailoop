// Package env resolves where the loop keeps its files.
package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	root       = ".ailoop"
	branchesIn = "branches"
	defaultDir = "default"
	stateFile  = "state.json"
)

// AILoopDir returns the directory holding this branch's loop state.
//
// State is per-branch so two lines of work do not overwrite each other's spec
// and design. It is a QUERY: it computes a path and touches nothing. An
// earlier version migrated files from the old layout here, which meant that
// asking where the state lived moved it - and it did so on every Load, every
// Save and every backup lookup. Migration is now Migrate, called once.
func AILoopDir(workspace string) string {
	branch := currentBranch(workspace)
	if branch == "" {
		return filepath.Join(workspace, root, defaultDir)
	}
	return filepath.Join(workspace, root, branchesIn, branch)
}

// RootDir returns the loop's directory for this workspace, the one that is
// not per-branch. Things shared by every line of work live here.
func RootDir(workspace string) string {
	return filepath.Join(workspace, root)
}

// SandboxDir returns the directory used for the isolated git worktree for this branch.
func SandboxDir(workspace string) string {
	return filepath.Join(AILoopDir(workspace), "sandbox")
}

// currentBranch returns the checked-out branch, sanitised for use as a
// directory name, or "" when there is none (no repository, or detached HEAD -
// which is what a throwaway worktree looks like).
func currentBranch(workspace string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return sanitise(branch)
}

// sanitise turns a branch name into one path segment.
//
// Git already forbids ".." inside a refname, but the directory is built from
// external input and a path that escapes .ailoop would be worse than an ugly
// name, so the check is here rather than assumed.
func sanitise(branch string) string {
	branch = strings.NewReplacer("/", "_", `\`, "_", ":", "_").Replace(branch)
	branch = strings.Trim(branch, ". ")
	if branch == "" || strings.Contains(branch, "..") {
		return ""
	}
	const maxLen = 100
	if len(branch) > maxLen {
		branch = branch[:maxLen]
	}
	return branch
}

// Migrate moves a pre-branch layout into the current branch directory.
//
// It reports what it did and every error it hit. The version this replaces
// discarded the results of four filesystem operations in a row: when the
// rename failed, the state stayed where it was, Load looked in the new place,
// and the user was told "no active AI Loop found" about work that was sitting
// on disk the whole time.
//
// Only the state moves. Backups are deliberately NOT per-branch - they hold
// the previous contents of the working tree, of which there is one - so
// moving them here would hide them from the rollback that needs them.
func Migrate(workspace string) (moved bool, err error) {
	old := filepath.Join(workspace, root, stateFile)
	if _, statErr := os.Stat(old); statErr != nil {
		return false, nil // nothing from the old layout
	}

	dir := AILoopDir(workspace)
	target := filepath.Join(dir, stateFile)

	// Never overwrite state that already belongs to this branch.
	if _, statErr := os.Stat(target); statErr == nil {
		return false, fmt.Errorf(
			"found an old %s and a current one at %s: move or delete the old one by hand",
			old, target)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.Rename(old, target); err != nil {
		return false, fmt.Errorf("moving %s to %s: %w", old, target, err)
	}
	return true, nil
}
