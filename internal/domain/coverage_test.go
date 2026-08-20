package domain_test

import (
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestFileCoveragePercentage(t *testing.T) {
	tests := []struct {
		name    string
		lines   map[int]int
		wantPct float64
	}{
		{name: "all hit", lines: map[int]int{1: 1, 2: 2, 3: 1}, wantPct: 100.0},
		{name: "half hit", lines: map[int]int{1: 1, 2: 0}, wantPct: 50.0},
		{name: "none hit", lines: map[int]int{1: 0, 2: 0}, wantPct: 0.0},
		{name: "empty", lines: map[int]int{}, wantPct: 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &domain.FileCoverage{Path: "f.lua", Lines: tc.lines}
			if got := f.Percentage(); got != tc.wantPct {
				t.Errorf("Percentage() = %.1f, want %.1f", got, tc.wantPct)
			}
		})
	}
}

func TestCoverageDataAggregate(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "a.lua", Lines: map[int]int{1: 1, 2: 0}}, // 1/2 hit
			{Path: "b.lua", Lines: map[int]int{1: 1, 2: 1}}, // 2/2 hit
		},
	}
	if cov.TotalLines() != 4 {
		t.Errorf("TotalLines() = %d, want 4", cov.TotalLines())
	}
	if cov.HitLines() != 3 {
		t.Errorf("HitLines() = %d, want 3", cov.HitLines())
	}
	if pct := cov.Percentage(); pct != 75.0 {
		t.Errorf("Percentage() = %.1f, want 75.0", pct)
	}
}

func TestCoverageDataPercentage_Empty(t *testing.T) {
	cov := &domain.CoverageData{}
	if pct := cov.Percentage(); pct != 0 {
		t.Errorf("Percentage() on empty CoverageData = %.1f, want 0", pct)
	}
}

func TestCoverageDataFileByPath(t *testing.T) {
	fc := &domain.FileCoverage{Path: "lua/init.lua", Lines: map[int]int{1: 1}}
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{fc}}

	if got := cov.FileByPath("lua/init.lua"); got != fc {
		t.Error("FileByPath returned wrong file")
	}
	if got := cov.FileByPath("missing.lua"); got != nil {
		t.Errorf("FileByPath for missing path should return nil, got %v", got)
	}
}

// Duplicate paths are the norm, not an edge case: every test file runs in its
// own nvim subprocess and each reports coverage for every source file it
// touched. Summing them double-counts both hits and totals, and the two do not
// scale together — subprocess A may hit lines {1,2} of a file while B hits
// {1,3}, giving 4 "hit lines" for a file that has 3.
func TestCoverageData_Normalize_MergesDuplicatePaths(t *testing.T) {
	c := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: "a.lua", Lines: map[int]int{1: 1, 2: 1, 3: 0}},
		{Path: "a.lua", Lines: map[int]int{1: 2, 2: 0, 3: 1}},
	}}

	c.Normalize()

	if len(c.Files) != 1 {
		t.Fatalf("Files = %d entries, want 1 after merge", len(c.Files))
	}
	got := c.Files[0].Lines
	want := map[int]int{1: 3, 2: 1, 3: 1} // counts summed, union of lines
	for line, w := range want {
		if got[line] != w {
			t.Errorf("line %d = %d hits, want %d", line, got[line], w)
		}
	}
	if c.TotalLines() != 3 {
		t.Errorf("TotalLines = %d, want 3", c.TotalLines())
	}
	if c.HitLines() != 3 {
		t.Errorf("HitLines = %d, want 3", c.HitLines())
	}
}

// A line recorded as executable-but-unhit in one subprocess and hit in another
// must end up hit — the zero must never mask a real execution.
func TestCoverageData_Normalize_ZeroDoesNotMaskHit(t *testing.T) {
	c := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: "b.lua", Lines: map[int]int{7: 0}},
		{Path: "b.lua", Lines: map[int]int{7: 5}},
	}}

	c.Normalize()

	if got := c.Files[0].Lines[7]; got != 5 {
		t.Errorf("line 7 = %d hits, want 5 (zero must not mask a hit)", got)
	}
	if c.HitLines() != 1 {
		t.Errorf("HitLines = %d, want 1", c.HitLines())
	}
}

// Normalize must be stable: repeated calls cannot change the result.
func TestCoverageData_Normalize_Idempotent(t *testing.T) {
	c := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: "c.lua", Lines: map[int]int{1: 1}},
		{Path: "c.lua", Lines: map[int]int{2: 0}},
	}}

	c.Normalize()
	first := c.Percentage()
	c.Normalize()

	if second := c.Percentage(); first != second {
		t.Errorf("Percentage changed on second Normalize: %v -> %v", first, second)
	}
}

// The same file arrives spelled differently depending on how it entered the
// report: the line hook records whatever debug.getinfo saw, which may carry a
// doubled slash from a concatenated package.path, while discovered source
// files arrive clean. Both must land in one entry.
func TestCoverageData_Normalize_MergesEquivalentPathSpellings(t *testing.T) {
	c := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: "/repo//lua/mod.lua", Lines: map[int]int{1: 1}},
		{Path: "/repo/lua/mod.lua", Lines: map[int]int{2: 0}},
	}}

	c.Normalize()

	if len(c.Files) != 1 {
		t.Fatalf("Files = %d, want 1 (equivalent spellings must merge)", len(c.Files))
	}
	if c.TotalLines() != 2 {
		t.Errorf("TotalLines = %d, want 2 (not double-counted)", c.TotalLines())
	}
	if got := c.Files[0].Path; got != "/repo/lua/mod.lua" {
		t.Errorf("Path = %q, want the cleaned form", got)
	}
}
