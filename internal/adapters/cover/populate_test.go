package cover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

// writeSource writes src to a file in a t.TempDir directory and returns
// the absolute path. Used to give PopulateBranches something to read.
func writeSource(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestPopulateBranchesIfElse(t *testing.T) {
	// then arm on line 2 hit twice, else arm on line 4 hit once.
	// The `if` on line 1 should get two arms with those exact taken counts.
	src := "if x then\n  y = 1\nelse\n  y = 2\nend"
	path := writeSource(t, "if.lua", src)
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: path, Lines: map[int]int{1: 3, 2: 2, 4: 1}},
	}}

	PopulateBranches(cov)

	f := cov.Files[0]
	if len(f.Branches) != 1 {
		t.Fatalf("branches = %d, want 1", len(f.Branches))
	}
	arms := f.Branches[0].Arms
	if len(arms) != 2 {
		t.Fatalf("arms = %d, want 2", len(arms))
	}
	if arms[0].Taken != 2 {
		t.Errorf("then-arm Taken = %d, want 2", arms[0].Taken)
	}
	if arms[1].Taken != 1 {
		t.Errorf("else-arm Taken = %d, want 1", arms[1].Taken)
	}
}

func TestPopulateBranchesIfNoElseIsUnknownForFallthrough(t *testing.T) {
	src := "if x then\n  y = 1\nend"
	path := writeSource(t, "if_no_else.lua", src)
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: path, Lines: map[int]int{1: 5, 2: 3}},
	}}

	PopulateBranches(cov)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != 3 {
		t.Errorf("then-arm Taken = %d, want 3", arms[0].Taken)
	}
	if arms[1].Taken != -1 {
		t.Errorf("else-arm Taken = %d, want -1 (unknown, no locatable fall-through body)", arms[1].Taken)
	}
}

func TestPopulateBranchesWhileBody(t *testing.T) {
	src := "while x do\n  y = 1\nend"
	path := writeSource(t, "while.lua", src)
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: path, Lines: map[int]int{1: 1, 2: 7}},
	}}

	PopulateBranches(cov)

	arms := cov.Files[0].Branches[0].Arms
	if arms[0].Taken != 7 {
		t.Errorf("body arm Taken = %d, want 7", arms[0].Taken)
	}
	if arms[1].Taken != -1 {
		t.Errorf("exit arm Taken = %d, want -1", arms[1].Taken)
	}
}

func TestPopulateBranchesUnexecutedArmIsZeroNotUnknown(t *testing.T) {
	// Zero and unknown are different: zero means "we know this arm has a
	// body and the body never executed" (a real miss), unknown means "no
	// body to check" (BRDA `-`). This distinction drives BRH and the
	// reporter's `-` rendering.
	src := "if x then\n  y = 1\nelse\n  y = 2\nend"
	path := writeSource(t, "unhit.lua", src)
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: path, Lines: map[int]int{1: 0, 2: 0, 4: 0}},
	}}

	PopulateBranches(cov)

	for i, a := range cov.Files[0].Branches[0].Arms {
		if a.Taken != 0 {
			t.Errorf("arm[%d] Taken = %d, want 0 (line instrumented but never hit)", i, a.Taken)
		}
	}
}

func TestPopulateBranchesMissingFileSkipped(t *testing.T) {
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: "/nonexistent/does-not-exist.lua", Lines: map[int]int{1: 1}},
	}}

	PopulateBranches(cov)

	if cov.Files[0].Branches != nil {
		t.Errorf("expected nil Branches for missing file, got %v", cov.Files[0].Branches)
	}
}

func TestPopulateBranchesNilSafe(t *testing.T) {
	// Guarded against nil so callers don't have to special-case the "no
	// coverage requested" path.
	PopulateBranches(nil)
	PopulateBranches(&domain.CoverageData{Files: []*domain.FileCoverage{nil}})
}

func TestPopulateBranchesEmptyFile(t *testing.T) {
	path := writeSource(t, "empty.lua", "local x = 1")
	cov := &domain.CoverageData{Files: []*domain.FileCoverage{
		{Path: path, Lines: map[int]int{1: 1}},
	}}

	PopulateBranches(cov)

	if len(cov.Files[0].Branches) != 0 {
		t.Errorf("expected no branches for a straight-line file, got %d", len(cov.Files[0].Branches))
	}
}
