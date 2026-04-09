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
