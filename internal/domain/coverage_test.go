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
