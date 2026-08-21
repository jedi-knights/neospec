package domain_test

import (
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestFileCoverageTotalBranchesSumsArmsAcrossPoints(t *testing.T) {
	// Two branch points, 2 arms each — BRF should be 4, not 2.
	f := &domain.FileCoverage{Branches: []domain.BranchCoverage{
		{Line: 1, Arms: []domain.BranchArm{{Taken: 1}, {Taken: 0}}},
		{Line: 5, Arms: []domain.BranchArm{{Taken: 3}, {Taken: -1}}},
	}}
	if got := f.TotalBranches(); got != 4 {
		t.Errorf("TotalBranches() = %d, want 4", got)
	}
}

func TestFileCoverageHitBranchesExcludesUnknownAndZero(t *testing.T) {
	// Only Taken > 0 counts as hit. Taken == 0 means "arm's body was
	// instrumented but never executed"; Taken == -1 means "unknown, no
	// locatable body". Neither counts as a hit.
	f := &domain.FileCoverage{Branches: []domain.BranchCoverage{
		{Arms: []domain.BranchArm{{Taken: 5}, {Taken: 0}, {Taken: -1}, {Taken: 2}}},
	}}
	if got := f.HitBranches(); got != 2 {
		t.Errorf("HitBranches() = %d, want 2 (the two Taken > 0 arms)", got)
	}
}

func TestFileCoverageBranchHelpersEmpty(t *testing.T) {
	f := &domain.FileCoverage{}
	if got := f.TotalBranches(); got != 0 {
		t.Errorf("TotalBranches() empty = %d, want 0", got)
	}
	if got := f.HitBranches(); got != 0 {
		t.Errorf("HitBranches() empty = %d, want 0", got)
	}
}
