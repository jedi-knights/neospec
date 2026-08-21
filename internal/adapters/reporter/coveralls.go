package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/jedi-knights/neospec/internal/domain"
)

// Coveralls writes coverage data in the Coveralls JSON API format.
// https://docs.coveralls.io/api-reference
type Coveralls struct{}

// NewCoveralls creates a Coveralls reporter.
func NewCoveralls() *Coveralls { return &Coveralls{} }

// coverallsPayload is the top-level Coveralls JSON structure.
type coverallsPayload struct {
	RepoToken   string            `json:"repo_token,omitempty"`
	ServiceName string            `json:"service_name"`
	SourceFiles []coverallsSource `json:"source_files"`
}

// coverallsSource represents a single source file in the Coveralls format.
// Coverage is a sparse array where index is line number - 1, value is hit count
// or null for non-executable lines.
//
// Branches is a flat quadruple array — [line, block, branch, hits, ...] —
// per Coveralls' spec. Emitted only when the file has source-derived
// branch data so plain files render as line-only, matching the LCOV and
// Cobertura convention of not synthesising empty branch structures.
type coverallsSource struct {
	Name     string `json:"name"`
	Coverage []*int `json:"coverage"` // nil = not executable
	Branches []int  `json:"branches,omitempty"`
}

func (c *Coveralls) Write(_ context.Context, w io.Writer, _ *domain.SuiteResult, cov *domain.CoverageData) error {
	if cov == nil {
		cov = &domain.CoverageData{}
	}

	payload := coverallsPayload{
		ServiceName: "neospec",
	}

	for _, file := range cov.Files {
		if len(file.Lines) == 0 {
			continue
		}

		// Find the maximum line number to size the coverage array.
		maxLine := 0
		lines := make([]int, 0, len(file.Lines))
		for ln := range file.Lines {
			lines = append(lines, ln)
			if ln > maxLine {
				maxLine = ln
			}
		}
		sort.Ints(lines)

		// Coveralls coverage array is 0-indexed (line N is at index N-1).
		coverage := make([]*int, maxLine)
		for _, ln := range lines {
			hits := file.Lines[ln]
			coverage[ln-1] = &hits
		}

		payload.SourceFiles = append(payload.SourceFiles, coverallsSource{
			Name:     file.Path,
			Coverage: coverage,
			Branches: buildCoverallsBranches(file.Branches),
		})
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling coveralls JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// buildCoverallsBranches converts source-derived BranchCoverage into the
// flat [line, block, branch, hits, ...] quadruple array Coveralls expects.
//
// Block is fixed at 0 (neospec does not group branches into basic blocks),
// branch is the arm index within the branch point (0, 1, ...), hits is
// the arm's Taken count. Unknown arms (Taken == -1, from implicit
// fall-through) are recorded as 0 hits — Coveralls has no representation
// for "unknown", so an unscoreable arm is counted as a miss (same
// aggregate treatment as LCOV's `-` and Cobertura's 0%).
//
// Output is sorted by (line, column) for deterministic bytes across runs.
func buildCoverallsBranches(branches []domain.BranchCoverage) []int {
	if len(branches) == 0 {
		return nil
	}
	sorted := make([]domain.BranchCoverage, len(branches))
	copy(sorted, branches)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].Column < sorted[j].Column
	})

	out := make([]int, 0, 4*len(sorted)*2)
	for _, b := range sorted {
		for i, arm := range b.Arms {
			hits := arm.Taken
			if hits < 0 {
				hits = 0
			}
			out = append(out, b.Line, 0, i, hits)
		}
	}
	return out
}
