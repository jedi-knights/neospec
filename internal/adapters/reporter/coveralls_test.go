package reporter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/domain"
)

func TestCoveralls_Write_NilCov(t *testing.T) {
	var buf bytes.Buffer
	r := reporter.NewCoveralls()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, nil); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `"service_name"`) {
		t.Errorf("missing service_name:\n%s", got)
	}
	if !strings.Contains(got, `"neospec"`) {
		t.Errorf("missing service name value:\n%s", got)
	}
}

func TestCoveralls_Write_WithCoverage(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/init.lua", Lines: map[int]int{1: 3, 2: 0, 3: 1}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewCoveralls()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// Parse JSON to validate structure.
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}

	sourceFiles, ok := payload["source_files"].([]any)
	if !ok || len(sourceFiles) != 1 {
		t.Fatalf("expected 1 source_file, got: %v", payload["source_files"])
	}
	file := sourceFiles[0].(map[string]any)
	if file["name"] != "lua/init.lua" {
		t.Errorf("name = %v, want lua/init.lua", file["name"])
	}
	// Coverage array is 0-indexed; line 3 is index 2.
	coverage := file["coverage"].([]any)
	if len(coverage) != 3 {
		t.Errorf("coverage array length = %d, want 3", len(coverage))
	}
}

func TestCoveralls_Write_SkipsEmptyFile(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/empty.lua", Lines: map[int]int{}}, // no lines → skipped
			{Path: "lua/real.lua", Lines: map[int]int{1: 1}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewCoveralls()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if strings.Contains(buf.String(), "lua/empty.lua") {
		t.Errorf("file with empty Lines should be skipped:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "lua/real.lua") {
		t.Errorf("file with lines should appear:\n%s", buf.String())
	}
}
