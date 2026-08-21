package reporter_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/domain"
)

func TestLCOV_Write(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{
				Path:  "lua/init.lua",
				Lines: map[int]int{1: 2, 2: 0, 3: 1},
			},
		},
	}

	var buf bytes.Buffer
	r := reporter.NewLCOV()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	got := buf.String()
	for _, want := range []string{"SF:lua/init.lua", "DA:1,2", "DA:2,0", "DA:3,1", "end_of_record"} {
		if !strings.Contains(got, want) {
			t.Errorf("LCOV output missing %q\nGot:\n%s", want, got)
		}
	}
}

func TestLCOV_Write_NilCov(t *testing.T) {
	var buf bytes.Buffer
	r := reporter.NewLCOV()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, nil); err != nil {
		t.Fatalf("Write() with nil cov error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil cov, got: %s", buf.String())
	}
}

func TestLCOV_Write_MultipleFiles(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/a.lua", Lines: map[int]int{1: 1}},
			{Path: "lua/b.lua", Lines: map[int]int{2: 3}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewLCOV()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "SF:lua/a.lua") {
		t.Errorf("missing SF:lua/a.lua:\n%s", got)
	}
	if !strings.Contains(got, "SF:lua/b.lua") {
		t.Errorf("missing SF:lua/b.lua:\n%s", got)
	}
}

func TestConsole_Write(t *testing.T) {
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{
			{Name: "mymod > works", Status: domain.StatusPass},
			{Name: "mymod > fails", Status: domain.StatusFail, Error: "assertion failed"},
		},
	}
	cov := &domain.CoverageData{}

	var buf bytes.Buffer
	r := reporter.NewConsole(false) // no color for predictable output
	if err := r.Write(context.Background(), &buf, suite, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "mymod > works") {
		t.Errorf("console output missing passing test name\nGot:\n%s", got)
	}
	if !strings.Contains(got, "assertion failed") {
		t.Errorf("console output missing failure message\nGot:\n%s", got)
	}
}

func TestConsole_Write_WithCoverage(t *testing.T) {
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{
			{Name: "test", Status: domain.StatusPass},
		},
	}
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/mod.lua", Lines: map[int]int{1: 1, 2: 1, 3: 0, 4: 0}},
		},
	}

	var buf bytes.Buffer
	r := reporter.NewConsole(false)
	if err := r.Write(context.Background(), &buf, suite, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "Coverage:") {
		t.Errorf("expected Coverage line when TotalLines > 0:\n%s", got)
	}
}

func TestConsole_Write_AllStatuses(t *testing.T) {
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{
			{Name: "passing", Status: domain.StatusPass},
			{Name: "failing", Status: domain.StatusFail, Error: "fail"},
			{Name: "skipped", Status: domain.StatusSkip},
			{Name: "errored", Status: domain.StatusError, Error: "err"},
		},
	}

	var buf bytes.Buffer
	r := reporter.NewConsole(false)
	if err := r.Write(context.Background(), &buf, suite, &domain.CoverageData{}); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "passing") {
		t.Errorf("missing passing test:\n%s", got)
	}
	if !strings.Contains(got, "skipped") {
		t.Errorf("missing skipped test:\n%s", got)
	}
	if !strings.Contains(got, "errored") {
		t.Errorf("missing errored test:\n%s", got)
	}
}

// TestConsole_Write_CoverageColors exercises all colorForPct branches by
// varying coverage percentages across the color thresholds.
func TestConsole_Write_CoverageColors(t *testing.T) {
	// Each entry: line counts that produce the desired coverage percentage.
	// brightgreen (≥90%): 9/10 = 90%
	// green (≥75%): 8/10 = 80%
	// yellow (≥60%): 7/10 = 70%
	// orange (≥40%): 5/10 = 50%
	// red (<40%): 3/10 = 30%
	tests := []struct {
		name    string
		hitOf10 int // hit lines out of 10 total
	}{
		{"brightgreen", 10},
		{"green", 8},
		{"yellow", 7},
		{"orange", 5},
		{"red", 3},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			lines := make(map[int]int, 10)
			for i := 1; i <= 10; i++ {
				if i <= tc.hitOf10 {
					lines[i] = 1
				} else {
					lines[i] = 0
				}
			}
			cov := &domain.CoverageData{
				Files: []*domain.FileCoverage{{Path: "f.lua", Lines: lines}},
			}
			suite := &domain.SuiteResult{
				Tests: []domain.TestResult{{Status: domain.StatusPass}},
			}
			var buf bytes.Buffer
			r := reporter.NewConsole(false)
			if err := r.Write(context.Background(), &buf, suite, cov); err != nil {
				t.Fatalf("Write() error: %v", err)
			}
			if !strings.Contains(buf.String(), "Coverage:") {
				t.Errorf("expected Coverage line:\n%s", buf.String())
			}
		})
	}
}

func TestConsole_Write_Color(t *testing.T) {
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{
			{Name: "passing", Status: domain.StatusPass},
		},
	}
	// High-coverage data to trigger brightgreen color path.
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/mod.lua", Lines: map[int]int{1: 1, 2: 1, 3: 1, 4: 1, 5: 1}},
		},
	}

	var buf bytes.Buffer
	r := reporter.NewConsole(true) // color=true path
	if err := r.Write(context.Background(), &buf, suite, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	// ANSI escape codes should be present.
	if !strings.Contains(got, "\033[") {
		t.Errorf("expected ANSI codes with color=true:\n%s", got)
	}
}

// Function records must appear before line records and FNDA must key to the
// same names as FN, or viewers silently drop the function column.
func TestLCOV_WritesFunctionRecords(t *testing.T) {
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/mod.lua",
		Lines: map[int]int{2: 1, 6: 0},
		Functions: []domain.FunctionCoverage{
			{Name: "M.called", Line: 1, Count: 3},
			{Name: "M.never", Line: 5, Count: 0},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"FN:1,M.called", "FN:5,M.never",
		"FNDA:3,M.called", "FNDA:0,M.never",
		"FNF:2", "FNH:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if fn, da := strings.Index(out, "FN:1"), strings.Index(out, "DA:"); fn > da {
		t.Errorf("FN records must precede DA records; got FN at %d, DA at %d", fn, da)
	}
}

// Two functions sharing a recovered name would make FNDA ambiguous — a viewer
// attributes both counts to whichever record it read last.
func TestLCOV_DisambiguatesDuplicateFunctionNames(t *testing.T) {
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/mod.lua",
		Lines: map[int]int{1: 1},
		Functions: []domain.FunctionCoverage{
			{Name: "helper", Line: 3, Count: 1},
			{Name: "helper", Line: 9, Count: 0},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "FN:3,helper") || !strings.Contains(out, "FN:9,helper@9") {
		t.Errorf("duplicate names not disambiguated:\n%s", out)
	}
	if strings.Count(out, "FNDA:") != 2 {
		t.Errorf("want 2 FNDA records, got:\n%s", out)
	}
}

// A file with no functions must emit no function block at all: FNF:0 renders
// as a 0%-functions row, which reads as a gap rather than "not applicable".
func TestLCOV_OmitsFunctionBlockWhenNoFunctions(t *testing.T) {
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/data.lua",
		Lines: map[int]int{1: 1},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out := buf.String(); strings.Contains(out, "FNF:") || strings.Contains(out, "FN:") {
		t.Errorf("expected no function records, got:\n%s", out)
	}
}

func TestLCOV_Write_BranchRecords(t *testing.T) {
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/branchy.lua",
		Lines: map[int]int{1: 3, 2: 2, 4: 1},
		Branches: []domain.BranchCoverage{
			{Line: 1, Column: 1, Kind: "if", Arms: []domain.BranchArm{
				{Taken: 2, Label: "then"},
				{Taken: 1, Label: "else"},
			}},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"BRDA:1,0,0,2", "BRDA:1,0,1,1", "BRF:2", "BRH:2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestLCOV_Write_BranchUnknownArmRendersAsDash(t *testing.T) {
	// An arm with Taken == -1 means the arm's hit count could not be
	// derived (implicit fall-through). LCOV renders that as "-" and
	// excludes it from BRH but keeps it in BRF.
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
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "BRDA:1,0,0,3") {
		t.Errorf("missing hit arm BRDA:\n%s", out)
	}
	if !strings.Contains(out, "BRDA:1,0,1,-") {
		t.Errorf("missing unknown arm BRDA (should be dash):\n%s", out)
	}
	// BRF counts both arms; BRH counts only the known-hit one.
	if !strings.Contains(out, "BRF:2") || !strings.Contains(out, "BRH:1") {
		t.Errorf("BRF/BRH should be 2/1:\n%s", out)
	}
}

func TestLCOV_Write_NoBranchesEmitsNoBranchLines(t *testing.T) {
	// A file with no source-derived branches emits no BRDA/BRF/BRH — a
	// BRF:0/BRH:0 block would render as 0%-branches, which reads as a
	// gap rather than "not applicable".
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/plain.lua",
		Lines: map[int]int{1: 1},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	for _, absent := range []string{"BRDA:", "BRF:", "BRH:"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q in output:\n%s", absent, out)
		}
	}
}

func TestLCOV_Write_BranchesSortedByLine(t *testing.T) {
	// Deterministic output: branches must be sorted by line, then column,
	// regardless of insertion order. Otherwise the same coverage data
	// produces different LCOV bytes on different runs, breaking any
	// downstream tool that hashes the file.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/order.lua",
		Lines: map[int]int{1: 1, 5: 1, 10: 1},
		Branches: []domain.BranchCoverage{
			{Line: 10, Kind: "if", Arms: []domain.BranchArm{{Taken: 1}, {Taken: 0}}},
			{Line: 1, Kind: "if", Arms: []domain.BranchArm{{Taken: 1}, {Taken: 0}}},
			{Line: 5, Kind: "if", Arms: []domain.BranchArm{{Taken: 1}, {Taken: 0}}},
		},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.LCOV{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	i1 := strings.Index(out, "BRDA:1,")
	i5 := strings.Index(out, "BRDA:5,")
	i10 := strings.Index(out, "BRDA:10,")
	if i1 >= i5 || i5 >= i10 {
		t.Errorf("BRDA lines out of order (1,5,10 offsets = %d,%d,%d):\n%s", i1, i5, i10, out)
	}
}
