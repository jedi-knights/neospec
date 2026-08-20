package domain

import "path/filepath"

// FileCoverage holds line-level execution counts for one source file.
// Lines is a map from 1-based line number to the number of times that line
// was executed during the test run.
type FileCoverage struct {
	// Path is the file path as reported by Lua's debug.getinfo, typically
	// relative to the project root.
	Path string
	// Lines maps 1-based line numbers to execution counts.
	Lines map[int]int
}

// HitLines returns the number of lines executed at least once.
func (f *FileCoverage) HitLines() int {
	count := 0
	for _, hits := range f.Lines {
		if hits > 0 {
			count++
		}
	}
	return count
}

// TotalLines returns the number of instrumented lines.
func (f *FileCoverage) TotalLines() int {
	return len(f.Lines)
}

// Percentage returns the line coverage percentage for this file, or 0 if there
// are no instrumented lines.
func (f *FileCoverage) Percentage() float64 {
	total := f.TotalLines()
	if total == 0 {
		return 0
	}
	return float64(f.HitLines()) / float64(total) * 100
}

// CoverageData is the aggregated coverage for an entire test run.
type CoverageData struct {
	Files []*FileCoverage
}

// TotalLines returns the total number of instrumented lines across all files.
func (c *CoverageData) TotalLines() int {
	n := 0
	for _, f := range c.Files {
		n += f.TotalLines()
	}
	return n
}

// HitLines returns the total number of lines hit at least once across all files.
func (c *CoverageData) HitLines() int {
	n := 0
	for _, f := range c.Files {
		n += f.HitLines()
	}
	return n
}

// Percentage returns the overall line coverage percentage across all files,
// or 0 if no lines are instrumented.
func (c *CoverageData) Percentage() float64 {
	total := c.TotalLines()
	if total == 0 {
		return 0
	}
	return float64(c.HitLines()) / float64(total) * 100
}

// Normalize merges duplicate entries so each path appears exactly once.
//
// Duplicates are the normal case, not an edge case: every test file runs in
// its own Neovim subprocess, and each reports coverage for every source file
// it touched. Without merging, TotalLines and HitLines both sum across the
// duplicates, and they do not scale together — subprocess A may hit lines
// {1,2} of a file while B hits {1,3}, yielding 4 "hit lines" for a file that
// only has 3. The reported percentage is wrong in both directions depending on
// how execution happened to distribute.
//
// Line counts are summed, which also gives the right answer for the
// executable-but-unexecuted lines recorded as zero: 0 + 5 is 5, so a zero from
// one subprocess can never mask a hit from another.
//
// Order is preserved by first appearance so output stays deterministic.
// Idempotent — after the first call there are no duplicates left to merge.
func (c *CoverageData) Normalize() {
	if len(c.Files) < 2 {
		return
	}

	merged := make(map[string]*FileCoverage, len(c.Files))
	order := make([]string, 0, len(c.Files))

	for _, f := range c.Files {
		if f == nil {
			continue
		}
		// Key by the cleaned path. The same file is spelled differently
		// depending on how it entered the report: the line hook records
		// whatever debug.getinfo saw (which may carry a doubled slash from a
		// package.path built by concatenation), while discovered source files
		// arrive already absolute and clean. Without this, one file becomes two
		// entries and its lines are counted twice.
		key := filepath.Clean(f.Path)
		existing, ok := merged[key]
		if !ok {
			// Copy rather than alias: callers may still hold the original slice,
			// and mutating their maps in place would be a surprising side effect.
			lines := make(map[int]int, len(f.Lines))
			for line, hits := range f.Lines {
				lines[line] = hits
			}
			merged[key] = &FileCoverage{Path: key, Lines: lines}
			order = append(order, key)
			continue
		}
		for line, hits := range f.Lines {
			existing.Lines[line] += hits
		}
	}

	files := make([]*FileCoverage, 0, len(order))
	for _, path := range order {
		files = append(files, merged[path])
	}
	c.Files = files
}

// FileByPath returns the FileCoverage for a given path, or nil.
func (c *CoverageData) FileByPath(path string) *FileCoverage {
	for _, f := range c.Files {
		if f.Path == path {
			return f
		}
	}
	return nil
}
