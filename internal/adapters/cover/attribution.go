package cover

import "github.com/jedi-knights/neospec/internal/domain"

// ApplyBranchCounters overwrites BranchArm.Taken values on cov using
// runtime branch-hit counts collected under the _neospec_br counter
// global. Runs after PopulateBranches so instrumented data
// preferentially wins for arms whose counter fired; arms with no
// matching counter retain whatever PopulateBranches computed from
// line-hit derivation.
//
// This is the back half of the branch-instrumentation runtime: the
// rewriter injects `_neospec_br(N); ` at each arm body, the runtime
// increments _neospec_br_counts[N], and this function maps N back to
// the domain arm using the []Injection metadata the rewriter returned
// alongside the rewritten source.
//
// injections is the concatenation of every file's Rewrite result so a
// single call attributes counters for a whole run. Ordering does not
// matter — attribution is keyed on Injection.BranchID.
//
// Best-effort by design:
//   - counters without a matching Injection are silently dropped
//     (stale IDs from an earlier run, or IDs the runtime saw that this
//     process did not plan);
//   - Injections whose File is not in cov are silently skipped (the
//     file was rewritten but no runtime hit it, or a path-canonicalisation
//     mismatch prevented match);
//   - Injections whose (Line, Column) does not match any BranchCoverage
//     in the resolved file are silently skipped (should not happen in a
//     coherent run — the same source was used for both — but not
//     grounds for aborting attribution of the rest);
//   - Injections whose ArmIndex is out of range on the matched branch
//     are silently skipped (defensive against desync).
//
// Nil cov is a no-op so callers do not have to special-case the "no
// coverage requested" path.
func ApplyBranchCounters(cov *domain.CoverageData, injections []Injection, counters map[int]int) {
	if cov == nil || len(injections) == 0 || len(counters) == 0 {
		return
	}
	filesByPath := make(map[string]*domain.FileCoverage, len(cov.Files))
	for _, f := range cov.Files {
		if f != nil {
			filesByPath[f.Path] = f
		}
	}
	for _, inj := range injections {
		hits, ok := counters[inj.BranchID]
		if !ok {
			continue
		}
		file := filesByPath[inj.File]
		if file == nil {
			continue
		}
		arm := findArm(file.Branches, inj.Line, inj.Column, inj.ArmIndex)
		if arm != nil {
			arm.Taken = hits
		}
	}
}

// findArm locates the domain BranchArm at (line, column, armIdx) within
// a file's branches, or nil if no matching entry exists. Returns a
// pointer into the slice so callers can mutate Taken in place.
func findArm(branches []domain.BranchCoverage, line, column, armIdx int) *domain.BranchArm {
	for i := range branches {
		b := &branches[i]
		if b.Line != line || b.Column != column {
			continue
		}
		if armIdx < 0 || armIdx >= len(b.Arms) {
			return nil
		}
		return &b.Arms[armIdx]
	}
	return nil
}
