package cover

import (
	"os"

	"github.com/jedi-knights/neospec/internal/domain"
)

// PopulateBranches walks every FileCoverage in cov, reads its source from
// disk, runs branch detection, and attaches BranchCoverage records with
// taken counts derived from the file's existing line-level hit map.
//
// Best-effort: files that cannot be read (removed after the test run,
// path resolution mismatch) are silently skipped — their line coverage is
// preserved, they simply carry no branch data. Parse errors are also
// non-fatal: whatever branches survived recovery are still attached, so
// the reporter can render partial information rather than nothing.
//
// Runs after CoverageData.Normalize; running before would cause Normalize
// to drop the branches (it rebuilds FileCoverage values without copying
// them, since branches are source-derived and independent of merging).
func PopulateBranches(cov *domain.CoverageData) {
	if cov == nil {
		return
	}
	for _, f := range cov.Files {
		if f == nil {
			continue
		}
		src, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		branches, _ := FindBranches(f.Path, src)
		f.Branches = toDomainBranches(branches, f.Lines)
	}
}

// toDomainBranches converts detector output into the domain shape, filling
// in each arm's Taken count from line hits.
func toDomainBranches(branches []Branch, lines map[int]int) []domain.BranchCoverage {
	if len(branches) == 0 {
		return nil
	}
	out := make([]domain.BranchCoverage, 0, len(branches))
	for _, b := range branches {
		arms := make([]domain.BranchArm, len(b.Arms))
		for i, a := range b.Arms {
			arms[i] = domain.BranchArm{
				Taken: takenFor(a, lines),
				Label: a.Label,
			}
		}
		out = append(out, domain.BranchCoverage{
			Line: b.Line, Column: b.Column, Kind: b.Kind, Arms: arms,
		})
	}
	return out
}

// takenFor returns the hit count for arm, or -1 if we cannot determine it.
// An arm with FirstLine == 0 has no locatable body (implicit fall-through
// like an if without else, or a loop's exit arm); these are "unknown"
// rather than "zero". An arm whose FirstLine is not in the line map is
// also unknown (the line wasn't instrumented — usually a same-line
// construct where the decision and body share one line).
func takenFor(arm Arm, lines map[int]int) int {
	if arm.FirstLine == 0 {
		return -1
	}
	hits, ok := lines[arm.FirstLine]
	if !ok {
		return -1
	}
	return hits
}
