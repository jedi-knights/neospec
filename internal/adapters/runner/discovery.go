// Package runner implements ports.TestRunner. It discovers test files via glob
// patterns and executes each one in a headless Neovim subprocess with the
// embedded Lua harness.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Discover finds all files matching the given glob patterns. It returns
// absolute paths in discovery order. Patterns that match no files are
// silently skipped. Unreadable directories encountered during a recursive
// walk are also silently skipped — files inside them will not appear in
// results.
//
// Patterns may contain "**" to match zero or more directory levels. For
// example, "tests/unit/**/*_spec.lua" matches all *_spec.lua files at any
// depth under tests/unit/, including files directly in tests/unit/ itself.
// Only the first "**" occurrence in a pattern is expanded; patterns with
// multiple "**" segments are not supported. A bare "**" pattern with no
// trailing segment matches all non-directory files under the base directory.
//
// Discover respects ctx cancellation and deadline: if the context is done
// before all patterns are processed, the function returns ctx.Err().
func Discover(ctx context.Context, patterns []string) ([]string, error) {
	seen := make(map[string]struct{})
	var results []string

	for _, pattern := range patterns {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var matches []string
		var err error

		if strings.Contains(pattern, "**") {
			matches, err = globDoublestar(ctx, pattern)
		} else {
			matches, err = filepath.Glob(pattern)
		}
		if err != nil {
			return nil, fmt.Errorf("expanding pattern %q: %w", pattern, err)
		}

		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil {
				return nil, fmt.Errorf("resolving absolute path for %q: %w", m, err)
			}
			if _, dup := seen[abs]; dup {
				continue
			}
			seen[abs] = struct{}{}
			results = append(results, abs)
		}
	}

	return results, nil
}

// globDoublestar expands a glob pattern containing "**" by recursively walking
// the filesystem. "**" matches zero or more directory levels, so:
//
//	tests/unit/**/*_spec.lua
//
// matches *_spec.lua files at any depth under tests/unit/, including files
// directly in tests/unit/ itself (zero intervening directories).
//
// Only the first "**" occurrence is expanded. The sub-pattern after "**" is
// matched against each file's base name using filepath.Match, so it must not
// itself contain path separators.
//
// Unreadable directories are silently skipped; their contents will not appear
// in results. Context cancellation is checked on every visited path.
func globDoublestar(ctx context.Context, pattern string) ([]string, error) {
	if strings.Count(pattern, "**") > 1 {
		return nil, fmt.Errorf("pattern %q: multiple ** segments are not supported", pattern)
	}

	idx := strings.Index(pattern, "**")

	// filepath.Clean("") returns "." so an empty prefix means "walk from CWD".
	baseDir := filepath.Clean(pattern[:idx])

	// Everything after "**" is the file pattern, e.g. "*_spec.lua".
	// Strip a single leading separator so "**/*_spec.lua" → "*_spec.lua".
	filePat := pattern[idx+len("**"):]
	if len(filePat) > 0 && (filePat[0] == '/' || filePat[0] == filepath.Separator) {
		filePat = filePat[1:]
	}
	if filePat == "" {
		filePat = "*"
	}

	var matches []string
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, werr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if werr != nil {
			if path == baseDir {
				// Surface errors on the root itself — callers need to know when
				// the directory they explicitly provided is unreadable, as this
				// is indistinguishable from "no matches" otherwise.
				return werr
			}
			// Skip unreadable subdirectories without aborting the entire walk.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ok, matchErr := filepath.Match(filePat, filepath.Base(path))
		if matchErr != nil {
			return matchErr
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})

	if errors.Is(err, fs.ErrNotExist) {
		// Base directory does not exist — not an error, just no matches.
		return nil, nil
	}
	return matches, err
}
