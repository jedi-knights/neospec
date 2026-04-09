package reporter

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/jedi-knights/neospec/internal/domain"
)

// LCOV writes coverage data in LCOV tracefile format.
// See https://ltp.sourceforge.net/coverage/lcov/geninfo.1.php for the format.
// Test result data is not included in LCOV output — it is a coverage-only format.
type LCOV struct{}

// NewLCOV creates an LCOV reporter.
func NewLCOV() *LCOV { return &LCOV{} }

func (l *LCOV) Write(_ context.Context, w io.Writer, _ *domain.SuiteResult, cov *domain.CoverageData) error {
	if cov == nil {
		return nil
	}

	for _, file := range cov.Files {
		// TN: test name (empty is valid)
		fmt.Fprintln(w, "TN:")
		fmt.Fprintf(w, "SF:%s\n", file.Path)

		// Collect and sort line numbers for deterministic output.
		lines := make([]int, 0, len(file.Lines))
		for ln := range file.Lines {
			lines = append(lines, ln)
		}
		sort.Ints(lines)

		for _, ln := range lines {
			fmt.Fprintf(w, "DA:%d,%d\n", ln, file.Lines[ln])
		}

		fmt.Fprintf(w, "LH:%d\n", file.HitLines())
		fmt.Fprintf(w, "LF:%d\n", file.TotalLines())
		fmt.Fprintln(w, "end_of_record")
	}

	return nil
}
