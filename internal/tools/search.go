package tools

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Searching is what makes a repository that does not fit in context still
// explorable. Without it an agent can only describe files someone already
// handed it, which is how it ends up inventing the rest.

const (
	maxMatches   = 200
	maxFilesScan = 20000
	maxLineLen   = 400
)

// skipDirs are never walked. They are large, generated, and never the answer.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".direnv": true, ".ailoop": true, "dist": true, "build": true,
}

// matchPath reports whether a path matches a glob that may contain "**".
// filepath.Match alone cannot cross separators, which makes the common
// "**/*.go" unusable.
func matchPath(pattern, path string) bool {
	if pattern == "" {
		return true
	}
	pat := strings.Split(pattern, "/")
	seg := strings.Split(path, "/")
	return matchSegments(pat, seg)
}

func matchSegments(pat, seg []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// "**" absorbs any number of segments, including none.
			for i := 0; i <= len(seg); i++ {
				if matchSegments(pat[1:], seg[i:]) {
					return true
				}
			}
			return false
		}
		if len(seg) == 0 {
			return false
		}
		ok, err := filepath.Match(pat[0], seg[0])
		if err != nil || !ok {
			return false
		}
		pat, seg = pat[1:], seg[1:]
	}
	return len(seg) == 0
}

// Glob lists files under workspace matching the pattern.
func Glob(workspace, pattern string) (string, error) {
	var found []string
	scanned := 0

	err := filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable directory is skipped, not fatal
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		scanned++
		if scanned > maxFilesScan {
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(workspace, path)
		if relErr != nil {
			return nil
		}
		if matchPath(pattern, rel) {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if len(found) == 0 {
		return fmt.Sprintf("no files match %q", pattern), nil
	}
	if len(found) > maxMatches {
		return strings.Join(found[:maxMatches], "\n") +
			fmt.Sprintf("\n... (%d more, refine the pattern)", len(found)-maxMatches), nil
	}
	return strings.Join(found, "\n"), nil
}

// Grep searches file contents for a regular expression, optionally restricted
// to a glob. Results are file:line:text, which is what makes a claim checkable.
func Grep(workspace, pattern, glob string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regexp: %w", err)
	}

	var out strings.Builder
	matches, scanned := 0, 0

	walkErr := filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if matches >= maxMatches {
			return filepath.SkipAll
		}
		scanned++
		if scanned > maxFilesScan {
			return filepath.SkipAll
		}

		rel, relErr := filepath.Rel(workspace, path)
		if relErr != nil || !matchPath(glob, rel) {
			return nil
		}

		f, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if !re.MatchString(line) {
				continue
			}
			if len(line) > maxLineLen {
				line = line[:maxLineLen] + "..."
			}
			fmt.Fprintf(&out, "%s:%d:%s\n", rel, n, line)
			matches++
			if matches >= maxMatches {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}

	if matches == 0 {
		// An empty result is stated explicitly. "No output" and "I did not
		// look" are indistinguishable otherwise, and that is how an absence
		// gets reported as a fact.
		return fmt.Sprintf("no matches for %q%s", pattern, globNote(glob)), nil
	}
	if matches >= maxMatches {
		out.WriteString(fmt.Sprintf("... (stopped at %d matches, narrow the pattern)\n", maxMatches))
	}
	return out.String(), nil
}

func globNote(glob string) string {
	if glob == "" {
		return " (whole workspace)"
	}
	return fmt.Sprintf(" in %q", glob)
}
