package reporter_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/domain"
)

func TestCobertura_Write_NilCov(t *testing.T) {
	var buf bytes.Buffer
	r := reporter.NewCobertura()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, nil); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("missing XML declaration:\n%s", got)
	}
	if strings.Contains(got, "DOCTYPE") {
		t.Errorf("output must not contain DOCTYPE:\n%s", got)
	}
	if !strings.Contains(got, `<coverage`) {
		t.Errorf("missing <coverage> element:\n%s", got)
	}
	// nil cov → 0 lines
	if !strings.Contains(got, `lines-valid="0"`) {
		t.Errorf("expected lines-valid=0:\n%s", got)
	}
}

func TestCobertura_Write_WithCoverage(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/init.lua", Lines: map[int]int{1: 3, 2: 0, 3: 1}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewCobertura()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "lua/init.lua") {
		t.Errorf("missing file path:\n%s", got)
	}
	// 3 total lines, 2 hit → lines-valid=3 lines-covered=2
	if !strings.Contains(got, `lines-valid="3"`) {
		t.Errorf("expected lines-valid=3:\n%s", got)
	}
	if !strings.Contains(got, `lines-covered="2"`) {
		t.Errorf("expected lines-covered=2:\n%s", got)
	}
	// line numbers in output
	if !strings.Contains(got, `number="1"`) {
		t.Errorf("expected line number 1:\n%s", got)
	}
}

// TestCobertura_Write_EncodeError covers the xml.Encoder.Encode error branch.
func TestCobertura_Write_EncodeError(t *testing.T) {
	r := reporter.NewCobertura()
	err := r.Write(context.Background(), failingWriter{}, &domain.SuiteResult{}, nil)
	if err == nil {
		t.Fatal("Write() expected error on encode failure, got nil")
	}
}

func TestCobertura_Write_MultipleFiles(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/a.lua", Lines: map[int]int{1: 1, 2: 0}},
			{Path: "lua/b.lua", Lines: map[int]int{1: 1, 2: 1}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewCobertura()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "lua/a.lua") {
		t.Errorf("missing lua/a.lua:\n%s", got)
	}
	if !strings.Contains(got, "lua/b.lua") {
		t.Errorf("missing lua/b.lua:\n%s", got)
	}
}

func TestCobertura_Write_BranchAttributesOnBranchLines(t *testing.T) {
	// One branch at line 1 with both arms hit → the <line number="1"> should
	// carry branch="true" and condition-coverage="100% (2/2)".
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/branchy.lua",
		Lines: map[int]int{1: 3, 2: 2, 4: 1},
		Branches: []domain.BranchCoverage{
			{Line: 1, Kind: "if", Arms: []domain.BranchArm{
				{Taken: 2, Label: "then"},
				{Taken: 1, Label: "else"},
			}},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.Cobertura{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		`branches-valid="2"`,
		`branches-covered="2"`,
		`branch="true"`,
		`condition-coverage="100% (2/2)"`,
		`<condition number="0" type="jump" coverage="100%">`,
		`<condition number="1" type="jump" coverage="100%">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestCobertura_Write_UnknownArmCountsAsMiss(t *testing.T) {
	// Cobertura has no "unknown" state; an arm with Taken == -1 is scored
	// as a miss (0%) — same aggregate treatment as LCOV's `-` (excluded
	// from BRH but included in BRF).
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/noelse.lua",
		Lines: map[int]int{1: 3, 2: 3},
		Branches: []domain.BranchCoverage{
			{Line: 1, Kind: "if", Arms: []domain.BranchArm{
				{Taken: 3, Label: "then"},
				{Taken: -1, Label: "else"},
			}},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.Cobertura{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `branches-covered="1"`) {
		t.Errorf("expected branches-covered=1 (unknown arm not counted):\n%s", got)
	}
	if !strings.Contains(got, `condition-coverage="50% (1/2)"`) {
		t.Errorf("expected 50%% condition coverage:\n%s", got)
	}
	if !strings.Contains(got, `coverage="0%"`) {
		t.Errorf("expected unknown arm rendered as 0%%:\n%s", got)
	}
}

func TestCobertura_Write_NonBranchLineHasNoBranchAttrs(t *testing.T) {
	// A file with only line coverage (no Branches populated) must not emit
	// `branch="true"` or `condition-coverage` on any line — otherwise
	// consumers show 0%-branches for straight-line files, which reads as
	// a gap rather than "not applicable".
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/plain.lua",
		Lines: map[int]int{1: 1, 2: 1},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.Cobertura{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, `branch="true"`) {
		t.Errorf("unexpected branch attr on plain line:\n%s", got)
	}
	if strings.Contains(got, "condition-coverage") {
		t.Errorf("unexpected condition-coverage on plain line:\n%s", got)
	}
	if strings.Contains(got, "<condition") {
		t.Errorf("unexpected <condition> element:\n%s", got)
	}
	// Aggregate branch counters should still be zero, not missing.
	if !strings.Contains(got, `branches-valid="0"`) || !strings.Contains(got, `branches-covered="0"`) {
		t.Errorf("expected zero branch counts:\n%s", got)
	}
}

func TestCobertura_Write_BranchRateRollsUp(t *testing.T) {
	// 3 arms total, 2 hit → branch-rate = 0.6666... at every level
	// (root/package/class), since we have one file in one package.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/mixed.lua",
		Lines: map[int]int{1: 1, 2: 1, 3: 0},
		Branches: []domain.BranchCoverage{
			{Line: 1, Kind: "if", Arms: []domain.BranchArm{
				{Taken: 1}, {Taken: 0},
			}},
			{Line: 2, Kind: "while", Arms: []domain.BranchArm{
				{Taken: 1}, {Taken: -1},
			}},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.Cobertura{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `branches-valid="4"`) {
		t.Errorf("expected branches-valid=4 (2 branches × 2 arms):\n%s", got)
	}
	if !strings.Contains(got, `branches-covered="2"`) {
		t.Errorf("expected branches-covered=2:\n%s", got)
	}
}
