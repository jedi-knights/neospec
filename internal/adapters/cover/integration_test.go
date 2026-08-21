package cover_test

// End-to-end integration test for the branch-instrumentation pipeline.
//
// Wires the real cover primitives (Rewrite, RewriteAll, PopulateBranches,
// ApplyBranchCounters) to the real Runner via WithSourceRewriter, but
// substitutes a scripted CommandRunner that returns canned reporter JSON
// simulating a Neovim run. The whole pipeline is exercised — the only
// thing missing is Neovim actually executing rewritten Lua.
//
// Kept out of the runner package so it can freely import cover; runner
// itself cannot import cover (cover imports runner for its companion
// executor). Uses cover_test as an external test package.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/cover"
	"github.com/jedi-knights/neospec/internal/adapters/runner"
	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/domain"
)

// scriptedRunner implements ports.CommandRunner by returning fixed
// bytes for stdout regardless of what was invoked. Used to feed a
// canned reporter JSON into the Runner without spawning Neovim.
type scriptedRunner struct{ stdout []byte }

func (s *scriptedRunner) Run(_ context.Context, _ []string, _ string, _ ...string) ([]byte, []byte, error) {
	return s.stdout, nil, nil
}

// writeFile writes contents to dir/name and returns the absolute path.
// The rewriter and Runner both use absolute paths internally so tests
// must too or the path-keyed maps miss.
func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	return abs
}

// TestBranchInstrumentation_EndToEnd wires the full Go pipeline —
// rewriter, runner, parse, attribution — with a canned reporter reply
// simulating a real run. Verifies that per-arm Taken counts end up
// where they should on the domain BranchCoverage, including for the
// elseif chain that the rewriter's arm-index bug used to corrupt.
func TestBranchInstrumentation_EndToEnd(t *testing.T) {
	// Arrange — a source file with three arms (if/elseif/else) at
	// distinct positions so the assertions can look up each arm
	// unambiguously.
	dir := t.TempDir()
	src := writeFile(t, dir, "mod.lua", `local M = {}
function M.classify(x)
  if x > 10 then
    return "high"
  elseif x > 5 then
    return "mid"
  else
    return "low"
  end
end
return M
`)

	// Precompute the injections so we know which BranchIDs to script
	// counter values for. This mirrors what the CLI does via
	// rewriteAllShim; running RewriteAll a second time inside the
	// runner's srcRewriterFn would produce identical output because
	// RewriteAll uses a deterministic sequential ID counter.
	rewritten, injsAny := shim([]string{src})
	injs := injsAny.([]cover.Injection)
	if len(injs) != 3 {
		t.Fatalf("expected 3 injections (then + elseif + else), got %d: %+v", len(injs), injs)
	}
	if _, ok := rewritten[src]; !ok {
		t.Fatalf("rewritten map missing source path: %v", rewritten)
	}

	// Simulate a Neovim run where classify() was called with 20, 15
	// (twice hitting the "high" arm) and 7 (once hitting "mid"). The
	// "low" arm is deliberately never taken.
	//
	// BranchID 1 = then body (M.classify(20/15) → "high" line 4)
	// BranchID 2 = elseif body (M.classify(7) → "mid" line 6)
	// BranchID 3 = else body (not hit; absent from the counter map)
	reply, err := json.Marshal(map[string]any{
		"tests":    []any{map[string]any{"name": "classify", "status": "pass", "duration_ms": 1.0}},
		"coverage": []any{map[string]any{"path": src, "lines": map[string]int{"4": 2, "6": 1, "8": 0}}},
		"br_counts": map[string]int{
			fmt.Sprintf("%d", injs[0].BranchID): 2,
			fmt.Sprintf("%d", injs[1].BranchID): 1,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Act — real Runner with scripted Neovim, real rewriter callback,
	// real cover primitives.
	r := runner.New(
		"/fake/nvim",
		sandbox.NewFactory(),
		&scriptedRunner{stdout: reply},
		false, "", nil,
	).
		WithCoverageSources([]string{src}).
		WithSourceRewriter(shim)

	testFile := writeFile(t, dir, "test/spec.lua", "-- placeholder; scripted runner ignores content\n")
	_, cov, err := r.Run(context.Background(), []string{testFile})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Runner should surface injections + branch counts.
	runInjs, ok := r.Injections().([]cover.Injection)
	if !ok || len(runInjs) != 3 {
		t.Fatalf("Runner.Injections = %v, want []cover.Injection of len 3", r.Injections())
	}
	counts := r.BranchCounts()
	if counts[injs[0].BranchID] != 2 || counts[injs[1].BranchID] != 1 {
		t.Errorf("BranchCounts = %v, want {%d:2, %d:1}", counts, injs[0].BranchID, injs[1].BranchID)
	}

	// Attribution — mirrors what commands.applyBranchInstrumentation
	// does after Run in the real CLI path.
	cover.PopulateBranches(cov)
	cover.ApplyBranchCounters(cov, runInjs, cov.BranchCounters)

	// Assert — verify the four arms (2 branches × 2 arms) all end up
	// with the correct Taken values. The elseif branch's arms are the
	// ones that used to be corrupted by the arm-index bug.
	file := findFile(t, cov, src)
	if len(file.Branches) != 2 {
		t.Fatalf("expected 2 branches (if + elseif), got %d: %+v", len(file.Branches), file.Branches)
	}
	ifBranch := findBranchAtLine(t, file, 3)
	elseifBranch := findBranchAtLine(t, file, 5)

	// if-branch: arm 0 (then, high) taken 2×, arm 1 (fallthrough to
	// elseif body line 6) taken 1× per line-hit derivation.
	if got := ifBranch.Arms[0].Taken; got != 2 {
		t.Errorf("if arm 0 (then, high) Taken = %d, want 2", got)
	}
	if got := ifBranch.Arms[1].Taken; got != 1 {
		t.Errorf("if arm 1 (fallthrough to elseif body) Taken = %d, want 1 (line 6 hits)", got)
	}

	// elseif-branch: arm 0 (then, mid) taken 1× — this is what the
	// arm-index bug used to write to arm 1 by mistake.
	if got := elseifBranch.Arms[0].Taken; got != 1 {
		t.Errorf("elseif arm 0 (then, mid) Taken = %d, want 1 (instrumented counter)", got)
	}
	// elseif-branch: arm 1 (fallthrough to else, low) taken 0× — line
	// 8 was never hit. Under the old bug this was overwritten to 1 by
	// the elseif body counter.
	if got := elseifBranch.Arms[1].Taken; got != 0 {
		t.Errorf("elseif arm 1 (fallthrough to else, low) Taken = %d, want 0 (line 8 not hit)", got)
	}
}

// findFile locates a FileCoverage by path or fails the test. Path
// matching is exact — CoverageData.Normalize keys on cleaned paths so
// a match is guaranteed when the source path was absolute at intake.
func findFile(t *testing.T, cov *domain.CoverageData, path string) *domain.FileCoverage {
	t.Helper()
	f := cov.FileByPath(path)
	if f == nil {
		var paths []string
		for _, fc := range cov.Files {
			paths = append(paths, fc.Path)
		}
		t.Fatalf("file %s not in cov (present: %v)", path, paths)
	}
	return f
}

// findBranchAtLine returns the branch whose decision is on the given
// source line, or fails the test. Branches are sorted by walk order so
// callers should not rely on index — this helper resolves by line
// which matches how a human reader identifies branches.
func findBranchAtLine(t *testing.T, file *domain.FileCoverage, line int) *domain.BranchCoverage {
	t.Helper()
	for i := range file.Branches {
		if file.Branches[i].Line == line {
			return &file.Branches[i]
		}
	}
	t.Fatalf("no branch at line %d in %s", line, file.Path)
	return nil
}

// shim adapts cover.RewriteAll's typed return to the runner's opaque
// SourceRewriteResolver. Test-side copy of the CLI's rewriteAllShim
// so the integration test doesn't depend on the cmd/neospec/commands
// package.
func shim(paths []string) (map[string]string, any) {
	sources, injections := cover.RewriteAll(paths)
	return sources, injections
}
