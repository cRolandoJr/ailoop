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
// planWrites is plan read as all-or-nothing: applying a patch is one
// operation, so the first refusal refuses it whole.
func planWrites(workspace string, blocks []Block, seen Seen) ([]pendingWrite, error) {
	writes, problems := plan(workspace, blocks, seen)
	if len(problems) > 0 {
		p := problems[0]
		return nil, fmt.Errorf("refusing patch to %s: %w", p.File, p.Err)
	}
	return writes, nil
}

// plan works out what these blocks would write, and what is wrong with each
// one that cannot be applied. It touches nothing on disk.
//
// One implementation, two readings: planWrites stops at the first problem
// because a patch applies whole, and DryRun reports them all because the
// review is per block. A dry run that disagreed with the real apply would be
// worse than none.
func plan(workspace string, blocks []Block, seen Seen) ([]pendingWrite, []Problem) {
	// Several blocks may target the same file. They apply in order, each on
	// the result of the previous one, which is what they would have done when
	// writing was interleaved.
	pending := map[string]*pendingWrite{}
	var order []string
	var problems []Problem

	// b is a copy: the same slice travels on to the review and to the real
	// apply, so planning must not leave a mark on it.
	for i, b := range blocks {
		rel, err := SafeRelPath(workspace, b.FilePath)
		if err != nil {
			problems = append(problems, Problem{Index: i, File: b.FilePath, Err: err})
			continue
		}

		w, known := pending[rel]
		if !known {
			w = &pendingWrite{rel: rel, full: filepath.Join(workspace, rel)}
			data, readErr := os.ReadFile(w.full)
			switch {
			case readErr == nil:
				w.content = string(data)
				// A nil seen means the caller opted out. An empty but non-nil
				// seen means the agent read nothing, and then no patch to an
				// existing file is legitimate.
				if seen != nil && !seen.Has(rel) {
					problems = append(problems, Problem{Index: i, File: b.FilePath,
						Err: unreadError(rel, seen)})
					continue
				}
			case os.IsNotExist(readErr):
				// A file that does not exist could not have been read, so the
				// read check does not apply: this is a creation.
				w.create = true
			default:
				problems = append(problems, Problem{Index: i, File: b.FilePath, Err: readErr})
				continue
			}
			pending[rel] = w
			order = append(order, rel)
		}

		next, err := applyTo(w, b)
		if err != nil {
			problems = append(problems, Problem{Index: i, File: b.FilePath, Err: err})
			continue
		}
		w.content = next
	}

	out := make([]pendingWrite, 0, len(order))
	for _, rel := range order {
		out = append(out, *pending[rel])
	}
	return out, problems
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
