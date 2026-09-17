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

	// Validate every path before touching the disk. Both checks run over all
	// blocks first: a patch that half-lands is worse than one that does not.
	for i := range blocks {
		rel, err := SafeRelPath(workspace, blocks[i].FilePath)
		if err != nil {
			return fmt.Errorf("refusing patch: %w", err)
		}
		blocks[i].FilePath = rel

		// A nil seen means the caller opted out of the check. An empty but
		// non-nil seen means the agent read nothing, and then no patch is
		// legitimate.
		if seen != nil && !seen.Has(rel) {
			// If the file doesn't exist on disk, they couldn't have read it. We allow creation.
			if _, statErr := os.Stat(filepath.Join(workspace, rel)); statErr == nil {
				return unreadError(rel, seen)
			}
		}
	}

	for _, b := range blocks {
		if err := applyBlock(workspace, b); err != nil {
			return fmt.Errorf("failed to apply patch to %s: %w", b.FilePath, err)
		}
	}
	return nil
}

// ParseBlocks parses the text proposal into blocks without applying them.
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

func applyBlock(workspace string, b Block) error {
	full := filepath.Join(workspace, b.FilePath)

	if err := Backup(workspace, b.FilePath); err != nil {
		return fmt.Errorf("backup failed, refusing to patch: %w", err)
	}

	contentBytes, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			if strings.TrimSpace(b.Search) != "" {
				return fmt.Errorf("file does not exist, so SEARCH block must be empty")
			}
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				return fmt.Errorf("failed to create directories: %w", err)
			}
			return os.WriteFile(full, []byte(b.Replace), 0644)
		}
		return fmt.Errorf("could not read file: %w", err)
	}
	content := string(contentBytes)

	n := strings.Count(content, b.Search)
	switch n {
	case 0:
		return errors.New("search block not found in file")
	case 1:
		// the only unambiguous case
	default:
		// Replacing "the first one" of several identical blocks patches a
		// location nobody chose. Refuse and let the model be more specific.
		return fmt.Errorf("search block is ambiguous: found %d occurrences, expected exactly 1", n)
	}

	return os.WriteFile(full, []byte(strings.Replace(content, b.Search, b.Replace, 1)), 0644)
}
