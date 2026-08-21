package cover

import (
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

// twoArmBranch is a small helper to make test setup less noisy — a
// branch with two arms whose initial Taken values reflect what
// PopulateBranches would have written (e.g., -1 for unknown, 0 for
// known-miss, positive for known-hit).
func twoArmBranch(line, col int, taken0, taken1 int) domain.BranchCoverage {
	return domain.BranchCoverage{
		Line: line, Column: col, Kind: "if",
		Arms: []domain.BranchArm{{Taken: taken0}, {Taken: taken1}},
	}
}

func TestApplyBranchCountersOverwritesTakenFromCounter(t *testing.T) {
	// Arrange — a file with a branch that PopulateBranches would have
	// scored as unknown (-1) for both arms because they're on the same
	// source line. Instrumentation gives us real per-arm counts.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, -1)},
	}}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 1, Column: 1, ArmIndex: 0, BranchID: 100},
		{File: "/src/a.lua", Line: 1, Column: 1, ArmIndex: 1, BranchID: 101},
	}
	counters := map[int]int{100: 7, 101: 3}

	// Act
	ApplyBranchCounters(cov, injections, counters)

	// Assert
	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != 7 || arms[1].Taken != 3 {
		t.Errorf("arms = %v, want [{7} {3}]", arms)
	}
}

func TestApplyBranchCountersOverwritesLineDerivedGuess(t *testing.T) {
	// Line-derived guess (5, 0) is a proxy; runtime says the truth is
	// (12, 4). Runtime wins because instrumentation is direct.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(2, 3, 5, 0)},
	}}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 2, Column: 3, ArmIndex: 0, BranchID: 1},
		{File: "/src/a.lua", Line: 2, Column: 3, ArmIndex: 1, BranchID: 2},
	}
	counters := map[int]int{1: 12, 2: 4}

	ApplyBranchCounters(cov, injections, counters)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != 12 || arms[1].Taken != 4 {
		t.Errorf("arms = %v, want [{12} {4}] (runtime overrides derived)", arms)
	}
}

func TestApplyBranchCountersPreservesArmsWithoutCounter(t *testing.T) {
	// Arm 0 was instrumented; arm 1 was not (no injection). Arm 1's
	// PopulateBranches-derived value must survive attribution.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, 5)},
	}}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 1, Column: 1, ArmIndex: 0, BranchID: 10},
	}
	counters := map[int]int{10: 8}

	ApplyBranchCounters(cov, injections, counters)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != 8 || arms[1].Taken != 5 {
		t.Errorf("arms = %v, want [{8} {5}] (arm 1 keeps derived value)", arms)
	}
}

func TestApplyBranchCountersUnknownIDsDropped(t *testing.T) {
	// A stale counter ID from an earlier run (or a runtime event we did
	// not plan) is silently dropped rather than failing attribution for
	// the rest of the file.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, -1)},
	}}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 1, Column: 1, ArmIndex: 0, BranchID: 100},
	}
	// Counter 999 has no injection; counter 100 does.
	counters := map[int]int{100: 3, 999: 42}

	ApplyBranchCounters(cov, injections, counters)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != 3 {
		t.Errorf("arm 0 = %d, want 3", arms[0].Taken)
	}
	if arms[1].Taken != -1 {
		t.Errorf("arm 1 = %d, want -1 (untouched)", arms[1].Taken)
	}
}

func TestApplyBranchCountersUnknownFileSkipped(t *testing.T) {
	// The injection references /src/b.lua but cov only has a.lua. The
	// counter is dropped rather than creating a phantom file entry.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, -1)},
	}}}
	injections := []Injection{
		{File: "/src/b.lua", Line: 1, Column: 1, ArmIndex: 0, BranchID: 1},
	}
	counters := map[int]int{1: 5}

	ApplyBranchCounters(cov, injections, counters)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != -1 {
		t.Errorf("arm untouched (foreign file injection ignored), got %d", arms[0].Taken)
	}
}

func TestApplyBranchCountersArmIndexOutOfRangeSkipped(t *testing.T) {
	// Defensive: an injection pointing at an arm index beyond the
	// branch's Arms length should not panic. Real code should not
	// produce this desync, but attribution should degrade gracefully.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, -1)},
	}}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 1, Column: 1, ArmIndex: 5, BranchID: 1},
	}
	counters := map[int]int{1: 10}

	// Should not panic.
	ApplyBranchCounters(cov, injections, counters)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != -1 || arms[1].Taken != -1 {
		t.Errorf("arms = %v, want unchanged", arms)
	}
}

func TestApplyBranchCountersUnmatchedPositionSkipped(t *testing.T) {
	// Injection Line/Column doesn't match any branch in the file's
	// Branches list. Silently skip — indicates a desync between the
	// rewriter's view of the source and PopulateBranches's.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path:     "/src/a.lua",
		Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, -1)},
	}}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 99, Column: 99, ArmIndex: 0, BranchID: 1},
	}
	counters := map[int]int{1: 5}

	ApplyBranchCounters(cov, injections, counters)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != -1 {
		t.Errorf("arm untouched, got %d", arms[0].Taken)
	}
}

func TestApplyBranchCountersNilInputs(t *testing.T) {
	// Every nil/empty combination is a valid no-op; callers should not
	// have to special-case any of them.
	ApplyBranchCounters(nil, nil, nil)

	cov := &domain.CoverageData{Files: []*domain.FileCoverage{{
		Path: "/src/a.lua", Branches: []domain.BranchCoverage{twoArmBranch(1, 1, 3, 4)},
	}}}
	// Empty injections — cov must not change.
	ApplyBranchCounters(cov, nil, map[int]int{1: 999})
	if cov.Files[0].Branches[0].Arms[0].Taken != 3 {
		t.Error("empty injections should be a no-op")
	}
	// Empty counters — cov must not change.
	ApplyBranchCounters(cov, []Injection{{BranchID: 1}}, nil)
	if cov.Files[0].Branches[0].Arms[0].Taken != 3 {
		t.Error("empty counters should be a no-op")
	}
}

func TestApplyBranchCountersMultipleFiles(t *testing.T) {
	// A single call attributes counters across all files in the run —
	// the CLI concatenates every file's Rewrite injections into one
	// slice and hands them here alongside the merged counter map.
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: "/src/a.lua", Branches: []domain.BranchCoverage{twoArmBranch(1, 1, -1, -1)}},
		{Path: "/src/b.lua", Branches: []domain.BranchCoverage{twoArmBranch(2, 2, -1, -1)}},
	}}
	injections := []Injection{
		{File: "/src/a.lua", Line: 1, Column: 1, ArmIndex: 0, BranchID: 1},
		{File: "/src/b.lua", Line: 2, Column: 2, ArmIndex: 1, BranchID: 2},
	}
	counters := map[int]int{1: 10, 2: 20}

	ApplyBranchCounters(cov, injections, counters)

	if cov.Files[0].Branches[0].Arms[0].Taken != 10 {
		t.Errorf("a.lua arm 0 = %d, want 10", cov.Files[0].Branches[0].Arms[0].Taken)
	}
	if cov.Files[1].Branches[0].Arms[1].Taken != 20 {
		t.Errorf("b.lua arm 1 = %d, want 20", cov.Files[1].Branches[0].Arms[1].Taken)
	}
}
