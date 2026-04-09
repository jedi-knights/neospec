package domain

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

// FileByPath returns the FileCoverage for a given path, or nil.
func (c *CoverageData) FileByPath(path string) *FileCoverage {
	for _, f := range c.Files {
		if f.Path == path {
			return f
		}
	}
	return nil
}
