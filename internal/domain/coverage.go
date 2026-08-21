package domain

import "path/filepath"

// FunctionCoverage describes one function definition in a source file.
//
// Name is best-effort: Lua prototypes carry no name, so it is recovered from
// the source line at Line and may be a positional label like "anonymous@42"
// for callbacks and multi-line signatures.
//
// Count is the execution count of the function's first body line, which
// equals the call count for any function whose first statement is
// unconditional.
type FunctionCoverage struct {
	Name  string
	Line  int
	Count int
}

// BranchCoverage describes one source-derived branch point in a file with
// per-arm hit information matched against line coverage.
type BranchCoverage struct {
	Line   int
	Column int
	Kind   string // "if" | "elseif" | "while" | "repeat" | "for"
	Arms   []BranchArm
}

// BranchArm is one alternative code path from a BranchCoverage. Taken == -1
// means the arm's hit count could not be derived from available line
// coverage (typically an implicit fall-through with no locatable body);
// consumers render it as "unknown" (e.g., `-` in LCOV BRDA).
type BranchArm struct {
	Taken int
	Label string
}

// FileCoverage holds line-level execution counts for one source file.
// Lines is a map from 1-based line number to the number of times that line
// was executed during the test run.
type FileCoverage struct {
	// Path is the file path as reported by Lua's debug.getinfo, typically
	// relative to the project root.
	Path string
	// Lines maps 1-based line numbers to execution counts.
	Lines map[int]int
	// Functions lists the functions defined in this file. May be empty for
	// files that define none.
	Functions []FunctionCoverage
	// Branches lists source-derived branch points, populated separately
	// from the runtime hook (see internal/adapters/cover.PopulateBranches).
	// May be empty for files that were not parsed or have no branches.
	Branches []BranchCoverage
}

// HitFunctions returns the number of functions executed at least once.
func (f *FileCoverage) HitFunctions() int {
	count := 0
	for _, fn := range f.Functions {
		if fn.Count > 0 {
			count++
		}
	}
	return count
}

// TotalFunctions returns the number of functions defined in this file.
func (f *FileCoverage) TotalFunctions() int {
	return len(f.Functions)
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

// TotalBranches returns the total number of branch arms across all branch
// points in this file (2 per if/while/for/etc.). Equal to LCOV's BRF.
func (f *FileCoverage) TotalBranches() int {
	n := 0
	for _, b := range f.Branches {
		n += len(b.Arms)
	}
	return n
}

// HitBranches returns the number of arms taken at least once. Arms whose
// Taken is -1 (unknown, no locatable body) do not count as hit. Equal to
// LCOV's BRH.
func (f *FileCoverage) HitBranches() int {
	n := 0
	for _, b := range f.Branches {
		for _, a := range b.Arms {
			if a.Taken > 0 {
				n++
			}
		}
	}
	return n
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
	// BranchCounters carries raw _neospec_br_counts values from a
	// branch-instrumented run. Not persisted to reports (the reporter
	// reads FileCoverage.Branches after ApplyBranchCounters has run);
	// exposed only so callers can pass it back to
	// cover.ApplyBranchCounters alongside the injection metadata. Nil
	// when instrumentation was inactive.
	BranchCounters map[int]int `json:"-"`
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

// TotalBranches returns the total number of branch arms across all files.
// Equal to LCOV's aggregate BRF.
func (c *CoverageData) TotalBranches() int {
	n := 0
	for _, f := range c.Files {
		n += f.TotalBranches()
	}
	return n
}

// HitBranches returns the total number of arms taken at least once across
// all files. Arms with Taken == -1 (unknown) do not count. Equal to LCOV's
// aggregate BRH.
func (c *CoverageData) HitBranches() int {
	n := 0
	for _, f := range c.Files {
		n += f.HitBranches()
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
			fns := make([]FunctionCoverage, len(f.Functions))
			copy(fns, f.Functions)
			merged[key] = &FileCoverage{Path: key, Lines: lines, Functions: fns}
			order = append(order, key)
			continue
		}
		for line, hits := range f.Lines {
			existing.Lines[line] += hits
		}
		// Functions are keyed by definition line: the same file analysed in two
		// subprocesses yields the same definitions, so merge counts rather than
		// appending duplicate records.
		mergeFunctions(existing, f.Functions)
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

// mergeFunctions folds src into dst.Functions, summing counts for functions
// already present (matched on definition line) and appending the rest.
func mergeFunctions(dst *FileCoverage, src []FunctionCoverage) {
	if len(src) == 0 {
		return
	}
	index := make(map[int]int, len(dst.Functions))
	for i, fn := range dst.Functions {
		index[fn.Line] = i
	}
	for _, fn := range src {
		if i, ok := index[fn.Line]; ok {
			dst.Functions[i].Count += fn.Count
			continue
		}
		index[fn.Line] = len(dst.Functions)
		dst.Functions = append(dst.Functions, fn)
	}
}
