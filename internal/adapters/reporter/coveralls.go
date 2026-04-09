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
type coverallsSource struct {
	Name     string `json:"name"`
	Coverage []*int `json:"coverage"` // nil = not executable
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
		})
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling coveralls JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
