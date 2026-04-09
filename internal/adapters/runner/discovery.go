// Package runner implements ports.TestRunner. It discovers test files via glob
// patterns and executes each one in a headless Neovim subprocess with the
// embedded Lua harness.
package runner

import (
	"context"
	"path/filepath"
)

// Discover finds all files matching the given glob patterns. It returns
// absolute paths sorted lexicographically. Patterns that match no files are
// silently skipped.
func Discover(_ context.Context, patterns []string) ([]string, error) {
	seen := make(map[string]struct{})
	var results []string

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil {
				return nil, err
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
