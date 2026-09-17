package patch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const backupSubdir = "backups"

func backupRoot(workspace string) string {
	return filepath.Join(workspace, ".ailoop", backupSubdir)
}

// Backup copies a file before it is patched, preserving its path relative to
// the workspace. The flat version of this function silently overwrote the
// backup of a/main.go with the one of b/main.go, which made Restore unable to
// know where anything belonged.
func Backup(workspace string, relPath string) error {
	src, err := os.Open(filepath.Join(workspace, relPath))
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to back up: the patch is creating this file. Record that
			// fact so Restore can delete it on rollback.
			return markAsNew(workspace, relPath)
		}
		return err
	}
	defer src.Close()

	dest := filepath.Join(backupRoot(workspace), relPath)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	// Never clobber an existing backup: the first one is the pre-loop state,
	// and that is the one a rollback must return to.
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

// markAsNew records a file that did not exist before the patch.
func markAsNew(workspace, relPath string) error {
	marker := filepath.Join(backupRoot(workspace), relPath+".ailoop-created")
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte("file did not exist before this loop\n"), 0644)
}

// RestoreResult reports what a rollback actually did, so the caller can tell
// the user the truth instead of a hopeful message.
type RestoreResult struct {
	Restored []string
	Deleted  []string
	Failed   map[string]error
}

// Restore puts every backed-up file back where it came from and deletes the
// files the patch created. It returns what it actually did.
func Restore(workspace string) (*RestoreResult, error) {
	root := backupRoot(workspace)
	res := &RestoreResult{Failed: map[string]error{}}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		return res, nil
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		// A created-file marker means: this file did not exist, remove it.
		if filepath.Ext(rel) == ".ailoop-created" {
			target := filepath.Join(workspace, rel[:len(rel)-len(".ailoop-created")])
			if rmErr := os.Remove(target); rmErr != nil && !os.IsNotExist(rmErr) {
				res.Failed[target] = rmErr
			} else {
				res.Deleted = append(res.Deleted, rel[:len(rel)-len(".ailoop-created")])
			}
			return nil
		}

		target := filepath.Join(workspace, rel)
		if copyErr := copyFile(path, target); copyErr != nil {
			res.Failed[rel] = copyErr
			return nil
		}
		res.Restored = append(res.Restored, rel)
		return nil
	})
	if err != nil {
		return res, err
	}

	// The backups are consumed by a rollback: keeping them would let a later
	// rollback restore a state that is no longer the one before the change.
	if err := os.RemoveAll(root); err != nil {
		return res, fmt.Errorf("restored, but could not clear backups: %w", err)
	}

	return res, nil
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
