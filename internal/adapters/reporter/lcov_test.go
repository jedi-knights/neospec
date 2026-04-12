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
