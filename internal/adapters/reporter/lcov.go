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

		writeFunctions(w, file)

		// Collect and sort line numbers for deterministic output.
		lines := make([]int, 0, len(file.Lines))
		for ln := range file.Lines {
			lines = append(lines, ln)
		}
		sort.Ints(lines)

		for _, ln := range lines {
			fmt.Fprintf(w, "DA:%d,%d\n", ln, file.Lines[ln])
		}

		writeBranches(w, file)

		fmt.Fprintf(w, "LH:%d\n", file.HitLines())
		fmt.Fprintf(w, "LF:%d\n", file.TotalLines())
		fmt.Fprintln(w, "end_of_record")
	}

	return nil
}

// writeBranches emits the BRDA/BRF/BRH block for one file.
//
// Each branch point contributes one BRDA line per arm in the form
// BRDA:<line>,<block>,<branch>,<taken>. Block is fixed at 0: neospec does
// not group branches into basic blocks, so every arm sits in its own
// nominal block. An unknown taken count (arm with no locatable body)
// renders as "-", matching the standard lcov convention — this keeps the
// arm in the BRF total without inflating BRH.
//
// Files with no branches emit nothing, mirroring writeFunctions: a
// BRF:0/BRH:0 block would render every data-only file as 0%-branches,
// which reads as a gap rather than "not applicable".
func writeBranches(w io.Writer, file *domain.FileCoverage) {
	if len(file.Branches) == 0 {
		return
	}

	branches := make([]domain.BranchCoverage, len(file.Branches))
	copy(branches, file.Branches)
	sort.Slice(branches, func(i, j int) bool {
		if branches[i].Line != branches[j].Line {
			return branches[i].Line < branches[j].Line
		}
		return branches[i].Column < branches[j].Column
	})

	for _, b := range branches {
		for i, arm := range b.Arms {
			taken := "-"
			if arm.Taken >= 0 {
				taken = fmt.Sprintf("%d", arm.Taken)
			}
			fmt.Fprintf(w, "BRDA:%d,0,%d,%s\n", b.Line, i, taken)
		}
	}
	fmt.Fprintf(w, "BRF:%d\n", file.TotalBranches())
	fmt.Fprintf(w, "BRH:%d\n", file.HitBranches())
}

// writeFunctions emits the FN/FNDA/FNF/FNH block for one file.
//
// lcov requires function records before line records, and keys FNDA to the
// FN name rather than the line. Names recovered from Lua source are not
// guaranteed unique within a file -- two local helpers can share a name, and
// several anonymous callbacks on one line would collide -- so duplicates are
// suffixed with their definition line. Without that, a viewer attributes every
// duplicate's hits to whichever record it saw last.
//
// Files with no functions emit nothing: FNF:0/FNH:0 is legal but makes every
// data-only file render as a 0%-functions row, which reads as a gap rather
// than as "not applicable".
func writeFunctions(w io.Writer, file *domain.FileCoverage) {
	if len(file.Functions) == 0 {
		return
	}

	fns := make([]domain.FunctionCoverage, len(file.Functions))
	copy(fns, file.Functions)
	sort.Slice(fns, func(i, j int) bool {
		if fns[i].Line != fns[j].Line {
			return fns[i].Line < fns[j].Line
		}
		return fns[i].Name < fns[j].Name
	})

	seen := make(map[string]int, len(fns))
	names := make([]string, len(fns))
	for i, fn := range fns {
		name := fn.Name
		if seen[name] > 0 {
			name = fmt.Sprintf("%s@%d", fn.Name, fn.Line)
		}
		seen[fn.Name]++
		names[i] = name
	}

	for i, fn := range fns {
		fmt.Fprintf(w, "FN:%d,%s\n", fn.Line, names[i])
	}
	for i, fn := range fns {
		fmt.Fprintf(w, "FNDA:%d,%s\n", fn.Count, names[i])
	}
	fmt.Fprintf(w, "FNF:%d\n", file.TotalFunctions())
	fmt.Fprintf(w, "FNH:%d\n", file.HitFunctions())
}
