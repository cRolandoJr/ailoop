package patch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideWorkspace means the model asked to write somewhere it must not.
var ErrOutsideWorkspace = errors.New("path escapes the workspace")

// SafeRelPath validates a path proposed by the model and returns it relative
// to the workspace.
//
// A model is allowed to PROPOSE any edit; it is not allowed to DECIDE where
// the edit lands. Absolute paths and traversal out of the workspace are
// refused here rather than trusted, because a confused model writing to
// /etc or ../../ is indistinguishable from a malicious one.
func SafeRelPath(workspace, proposed string) (string, error) {
	p := strings.TrimSpace(proposed)
	if p == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %q is absolute", ErrOutsideWorkspace, p)
	}

	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideWorkspace, proposed)
	}

	// Belt and braces: resolve against the workspace and confirm containment.
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absWorkspace, clean)
	if target != absWorkspace && !strings.HasPrefix(target, absWorkspace+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrOutsideWorkspace, proposed)
	}

	return clean, nil
}

// Block represents a single SEARCH/REPLACE operation.
type Block struct {
	FilePath string
	Search   string
	Replace  string
}

// Apply parses SEARCH/REPLACE blocks out of the proposal and applies them
// inside workspace. It applies nothing unless every block is valid: a patch
// that half-lands is worse than one that does not land.
//
// seen is the set of files the agent read. A nil seen disables the check,
// which is what a caller with no agent involved passes.
func Apply(workspace, proposal string, seen Seen) error {
	blocks, err := ParseBlocks(proposal)
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		return nil
	}

	return ApplyBlocks(workspace, blocks, seen)
}

// ApplyBlocks applies a pre-parsed list of blocks. seen works as in Apply.
func ApplyBlocks(workspace string, blocks []Block, seen Seen) error {
	if len(blocks) == 0 {
		return nil
	}

	writes, err := planWrites(workspace, blocks, seen)
	if err != nil {
		return err
	}

	// Nothing was touched until here. Every refusal above left the workspace
	// exactly as it was.
	for _, w := range writes {
		if err := Backup(workspace, w.rel); err != nil {
			return fmt.Errorf("backup failed, refusing to patch %s: %w", w.rel, err)
		}
		if w.create {
			if err := os.MkdirAll(filepath.Dir(w.full), 0755); err != nil {
				return fmt.Errorf("creating directories for %s: %w", w.rel, err)
			}
		}
		if err := os.WriteFile(w.full, []byte(w.content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", w.rel, err)
		}
	}
	return nil
}

// pendingWrite is one file's final content, computed but not yet written.
type pendingWrite struct {
	rel     string
	full    string
	content string
	create  bool
}

// planWrites validates every block and computes the resulting content of each
// file, without touching the disk.
//
// Every refusal lives here on purpose. Validating some things up front and the
// rest while writing is what let a patch half-land: blocks one and two were
// already on disk when block three turned out to have an ambiguous search.
// "All or nothing" is only true if every check runs before the first write.
func planWrites(workspace string, blocks []Block, seen Seen) ([]pendingWrite, error) {
	// Several blocks may target the same file. They apply in order, each on
	// the result of the previous one, which is what they would have done when
	// writing was interleaved.
	pending := map[string]*pendingWrite{}
	var order []string

	for i := range blocks {
		rel, err := SafeRelPath(workspace, blocks[i].FilePath)
		if err != nil {
			return nil, fmt.Errorf("refusing patch: %w", err)
		}
		blocks[i].FilePath = rel
		full := filepath.Join(workspace, rel)

		w, known := pending[rel]
		if !known {
			w = &pendingWrite{rel: rel, full: full}
			data, readErr := os.ReadFile(full)
			switch {
			case readErr == nil:
				w.content = string(data)
				// A nil seen means the caller opted out. An empty but non-nil
				// seen means the agent read nothing, and then no patch to an
				// existing file is legitimate.
				if seen != nil && !seen.Has(rel) {
					return nil, unreadError(rel, seen)
				}
			case os.IsNotExist(readErr):
				// A file that does not exist could not have been read, so the
				// read check does not apply: this is a creation.
				w.create = true
			default:
				return nil, fmt.Errorf("could not read %s: %w", rel, readErr)
			}
			pending[rel] = w
			order = append(order, rel)
		}

		next, err := applyTo(w, blocks[i])
		if err != nil {
			return nil, fmt.Errorf("refusing patch to %s: %w", rel, err)
		}
		w.content = next
	}

	out := make([]pendingWrite, 0, len(order))
	for _, rel := range order {
		out = append(out, *pending[rel])
	}
	return out, nil
}

// applyTo computes the content of one file after one block, or explains why
// the block cannot apply.
func applyTo(w *pendingWrite, b Block) (string, error) {
	if w.create && w.content == "" {
		if strings.TrimSpace(b.Search) != "" {
			return "", errors.New("file does not exist, so the SEARCH block must be empty")
		}
		return b.Replace, nil
	}

	switch n := strings.Count(w.content, b.Search); n {
	case 0:
		return "", errors.New("search block not found in file")
	case 1:
		return strings.Replace(w.content, b.Search, b.Replace, 1), nil
	default:
		// Replacing "the first one" of several identical blocks patches a
		// location nobody chose. Refuse and let the model be more specific.
		return "", fmt.Errorf("search block is ambiguous: found %d occurrences, expected exactly 1", n)
	}
}

func ParseBlocks(text string) ([]Block, error) {
	var blocks []Block

	parts := strings.Split(text, "<<<<\n")
	if len(parts) <= 1 {
		return nil, nil
	}

	for _, part := range parts[1:] {
		endIdx := strings.Index(part, ">>>>")
		if endIdx == -1 {
			return nil, errors.New("malformed patch: a block was opened with <<<< and never closed with >>>>")
		}

		blockStr := part[:endIdx]
		sections := strings.SplitN(blockStr, "====\n", 3)
		if len(sections) != 3 {
			return nil, errors.New("malformed patch: expected path ==== search ==== replace")
		}

		blocks = append(blocks, Block{
			FilePath: strings.TrimSpace(sections[0]),
			Search:   strings.TrimSuffix(sections[1], "\n"),
			Replace:  strings.TrimSuffix(sections[2], "\n"),
		})
	}

	return blocks, nil
}
