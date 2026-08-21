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

// TestCoveralls_Write_WriteError covers the fmt.Fprintln error return path.
func TestCoveralls_Write_WriteError(t *testing.T) {
	r := reporter.NewCoveralls()
	err := r.Write(context.Background(), failingWriter{}, &domain.SuiteResult{}, nil)
	if err == nil {
		t.Fatal("Write() expected error on write failure, got nil")
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

func TestCoveralls_Write_BranchesFlatQuadruples(t *testing.T) {
	// Coveralls' branches field is a flat [line, block, branch, hits, ...]
	// array — assert both the quadruple layout and the exact values so a
	// consumer parsing the array in groups of 4 reconstructs each arm.
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
	if err := (&reporter.Coveralls{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	file := payload["source_files"].([]any)[0].(map[string]any)
	branches, ok := file["branches"].([]any)
	if !ok {
		t.Fatalf("branches missing or wrong type: %v", file["branches"])
	}
	want := []float64{1, 0, 0, 2, 1, 0, 1, 1}
	if len(branches) != len(want) {
		t.Fatalf("branches len = %d, want %d: %v", len(branches), len(want), branches)
	}
	for i, w := range want {
		if branches[i].(float64) != w {
			t.Errorf("branches[%d] = %v, want %v", i, branches[i], w)
		}
	}
}

func TestCoveralls_Write_UnknownArmRecordedAsZero(t *testing.T) {
	// Coveralls has no "unknown" representation, so an arm we can't score
	// (Taken == -1, from an if-without-else fall-through) is recorded as
	// 0 hits — same aggregate treatment as LCOV's `-` and Cobertura's 0%.
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
	if err := (&reporter.Coveralls{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal(buf.Bytes(), &payload)
	branches := payload["source_files"].([]any)[0].(map[string]any)["branches"].([]any)
	// Last quadruple's hits (index 7) should be 0, not -1 or missing.
	if branches[7].(float64) != 0 {
		t.Errorf("unknown-arm hits = %v, want 0", branches[7])
	}
}

func TestCoveralls_Write_NoBranchesOmitsField(t *testing.T) {
	// A file with no source-derived branches must not emit an empty
	// branches array — Coveralls consumers that count "files with branch
	// data" would otherwise see every straight-line file as having zero
	// branches instead of "not applicable".
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:  "lua/plain.lua",
		Lines: map[int]int{1: 1},
	}}}

	var buf bytes.Buffer
	if err := (&reporter.Coveralls{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal(buf.Bytes(), &payload)
	file := payload["source_files"].([]any)[0].(map[string]any)
	if _, present := file["branches"]; present {
		t.Errorf("branches field should be omitted for plain files, got: %v", file["branches"])
	}
}

func TestCoveralls_Write_BranchesSortedByLine(t *testing.T) {
	// Deterministic output: branches inserted out-of-order must be sorted
	// by line before serialization so identical coverage always produces
	// identical bytes.
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
	if err := (&reporter.Coveralls{}).Write(context.Background(), &buf, nil, cov); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal(buf.Bytes(), &payload)
	branches := payload["source_files"].([]any)[0].(map[string]any)["branches"].([]any)
	// Each branch emits 2 arms × 4 numbers = 8 numbers per branch, so the
	// first quadruple of each branch sits at offsets 0, 8, 16.
	got := []float64{branches[0].(float64), branches[8].(float64), branches[16].(float64)}
	want := []float64{1, 5, 10}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line at quadruple %d: got %v, want %v", i, got[i], want[i])
		}
	}
}
