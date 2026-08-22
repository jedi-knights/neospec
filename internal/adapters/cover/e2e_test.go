package cover_test

// True end-to-end test for the branch-instrumentation runtime.
//
// Unlike integration_test.go — which uses a scripted CommandRunner to
// simulate Neovim's reply — this test invokes the real Neovim binary
// against real Lua source and verifies that _neospec_br counters
// actually fire when the rewritten source runs.
//
// Guarded by NEOSPEC_E2E environment variable rather than a build tag
// so the file always compiles (refactors catch issues) but the test
// itself only runs where a suitable Neovim is available — currently
// the dedicated GitHub Actions job installed via .github/workflows/ci.yml.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/cover"
	"github.com/jedi-knights/neospec/internal/adapters/runner"
	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/domain"
)

// realNvimProvider returns a fixed nvim path (the one exec.LookPath
// found) without downloading or caching. Matches the ports.NeovimProvider
// interface but skips provisioning — the E2E CI job installs nvim
// once at job setup, so the provider's job here is just to hand the
// path back to cover.Executor.
type realNvimProvider struct{ path string }

func (r *realNvimProvider) Ensure(_ context.Context, _ domain.Version, _ domain.Platform) (string, error) {
	return r.path, nil
}

// realE2ERunner wraps runner.RunCommand so cover.Executor can drive a
// real subprocess. Duplicated from cmd/neospec/commands/exec.go's
// realRunner (unexported there) because that package would drag the
// whole CLI into this test's dependency graph.
type realE2ERunner struct{}

func (realE2ERunner) Run(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	return runner.RunCommand(ctx, env, name, args...)
}

// TestBranchInstrumentation_TrueE2E runs a real Neovim against a
// small program with an if/elseif/else, checks that branch counters
// actually fire, and verifies attribution produces the expected
// per-arm Taken values.
//
// Skipped unless NEOSPEC_E2E is set and `nvim` is on PATH. This
// keeps `go test ./...` fast and Neovim-free for anyone running
// locally without the E2E environment.
func TestBranchInstrumentation_TrueE2E(t *testing.T) {
	if os.Getenv("NEOSPEC_E2E") == "" {
		t.Skip("NEOSPEC_E2E env var not set — skipping true-e2e test")
	}
	nvimPath, err := exec.LookPath("nvim")
	if err != nil {
		t.Skipf("nvim not on PATH: %v", err)
	}

	dir := t.TempDir()

	// Real source file with three arms. The rewriter will inject a
	// counter at each arm body; runtime will fire counter 1 (high)
	// twice, counter 2 (mid) once, counter 3 (low) not at all.
	srcFile := writeE2EFile(t, dir, "mod.lua", `local M = {}
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

	// require (not dofile) so the module goes through package.loaders —
	// which is where the coverage hook's shim lives. dofile bypasses
	// the shim entirely and would load the on-disk (unrewritten)
	// source, silently defeating instrumentation. Prepend the source
	// dir to package.path so require("mod") resolves to srcFile.
	// describe/it/assert come from neospec's harness which loads
	// before this file runs.
	testContent := fmt.Sprintf(`package.path = %q .. package.path
local M = require("mod")
describe("classify", function()
  it("high", function()
    assert.equals("high", M.classify(20))
    assert.equals("high", M.classify(15))
  end)
  it("mid", function()
    assert.equals("mid", M.classify(7))
  end)
end)
`, dir+"/?.lua;")
	testFile := writeE2EFile(t, dir, "test/mod_spec.lua", testContent)

	// Real runner with real Neovim and real rewriter — nothing
	// scripted. The verify-in-CI job is what makes this actionable;
	// locally it just skips.
	r := runner.NewWithDefaultSandbox(nvimPath, false, "", nil).
		WithCoverageSources([]string{srcFile}).
		WithSourceRewriter(func(paths []string) (map[string]string, any) {
			sources, injections := cover.RewriteAll(paths)
			return sources, injections
		})

	suite, cov, err := r.Run(context.Background(), []string{testFile})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Every rewritten arm must be reachable syntactically — a broken
	// rewrite typically manifests as a Lua parse error from Neovim,
	// which surfaces as a test-runner failure rather than counter
	// data. Assert on the shape first so the diagnosis is obvious
	// when this fails.
	if len(suite.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d — was rewritten source valid?\ntests: %+v",
			len(suite.Tests), suite.Tests)
	}
	for _, tr := range suite.Tests {
		if tr.Status != domain.StatusPass {
			t.Errorf("test %s: status=%v error=%s", tr.Name, tr.Status, tr.Error)
		}
	}

	// Runner should have collected the injection metadata and counter
	// map from the real subprocess.
	injections, ok := r.Injections().([]cover.Injection)
	if !ok || len(injections) != 3 {
		t.Fatalf("Injections = %v (%T), want []cover.Injection of len 3",
			r.Injections(), r.Injections())
	}
	counts := r.BranchCounts()
	if len(counts) == 0 {
		t.Fatal("BranchCounts empty — did _neospec_br actually fire under real Neovim?")
	}

	// Per-counter expectations: high=2, mid=1, low absent.
	assertCount(t, counts, injections[0].BranchID, 2, "high (then arm)")
	assertCount(t, counts, injections[1].BranchID, 1, "mid (elseif arm)")
	if hit, present := counts[injections[2].BranchID]; present {
		t.Errorf("low arm counter should be absent (never taken), got %d", hit)
	}

	// Attribution — mirrors the CLI's applyBranchInstrumentation step.
	cover.PopulateBranches(cov)
	cover.ApplyBranchCounters(cov, injections, cov.BranchCounters)

	// Final per-arm assertions on the domain model. These are the
	// numbers that would land in an LCOV BRDA / Cobertura condition-
	// coverage / Coveralls branches array — the whole reason this
	// pipeline exists.
	file := cov.FileByPath(srcFile)
	if file == nil {
		t.Fatalf("source file not in coverage: paths=%v", coveragePaths(cov))
	}
	if len(file.Branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(file.Branches))
	}
	ifBranch := branchAtLine(t, file, 3)
	elseifBranch := branchAtLine(t, file, 5)
	if got := ifBranch.Arms[0].Taken; got != 2 {
		t.Errorf("if arm 0 (high) Taken = %d, want 2", got)
	}
	if got := elseifBranch.Arms[0].Taken; got != 1 {
		t.Errorf("elseif arm 0 (mid) Taken = %d, want 1", got)
	}
}

// writeE2EFile is a helper matching the pattern in integration_test.go
// but named distinctly so both files can coexist in the same package.
func writeE2EFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	return abs
}

// assertCount pins one counter value with a human-friendly label so
// a failure names the arm rather than an opaque numeric ID.
func assertCount(t *testing.T, counts map[int]int, id, want int, label string) {
	t.Helper()
	if got := counts[id]; got != want {
		t.Errorf("counter %d (%s) = %d, want %d", id, label, got, want)
	}
}

// branchAtLine returns the branch whose decision is on the given line.
// Duplicated from integration_test.go's helper — kept local so the
// e2e file can be read standalone.
func branchAtLine(t *testing.T, file *domain.FileCoverage, line int) *domain.BranchCoverage {
	t.Helper()
	for i := range file.Branches {
		if file.Branches[i].Line == line {
			return &file.Branches[i]
		}
	}
	t.Fatalf("no branch at line %d in %s", line, file.Path)
	return nil
}

// coveragePaths lists all recorded coverage file paths for diagnostic
// output when a file lookup fails.
func coveragePaths(cov *domain.CoverageData) []string {
	out := make([]string, 0, len(cov.Files))
	for _, f := range cov.Files {
		out = append(out, f.Path)
	}
	return out
}

// TestBranchInstrumentation_TrueE2E_CoverMode runs real Neovim + real
// plenary-busted against a source with an if/elseif/else, driven
// through cover.Executor (not the runner package). Verifies the
// wrapped-runner path emits BRDA with the same per-arm accuracy the
// run-mode E2E pinned in #41. Without this, cover-mode branch
// attribution could regress silently — every scripted integration
// test would pass while real Neovim + real plenary produced empty
// counters.
//
// Guarded by NEOSPEC_E2E (same as the run-mode E2E tests) plus a
// plenary.nvim install at /tmp/plenary.nvim (overridable via
// NEOSPEC_E2E_PLENARY). The E2E CI job installs plenary at the
// default path; local runs skip if plenary isn't there.
func TestBranchInstrumentation_TrueE2E_CoverMode(t *testing.T) {
	if os.Getenv("NEOSPEC_E2E") == "" {
		t.Skip("NEOSPEC_E2E env var not set — skipping true-e2e test")
	}
	nvimPath, err := exec.LookPath("nvim")
	if err != nil {
		t.Skipf("nvim not on PATH: %v", err)
	}
	plenaryPath := os.Getenv("NEOSPEC_E2E_PLENARY")
	if plenaryPath == "" {
		plenaryPath = "/tmp/plenary.nvim"
	}
	if _, err := os.Stat(plenaryPath); err != nil {
		t.Skipf("plenary.nvim not at %s: %v", plenaryPath, err)
	}

	dir := t.TempDir()

	// mod.lua under lua/ so `require("mod")` resolves via package.path.
	srcFile := writeE2EFile(t, dir, "lua/mod.lua", `local M = {}
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

	// Test file in plenary-busted format (describe/it/assert). Exercises
	// the "high" arm twice and the "mid" arm once; leaves "low"
	// untouched. Matches the run-mode E2E's arm-hit pattern so the
	// attribution assertions read the same way.
	testDir := filepath.Join(dir, "tests")
	writeE2EFile(t, dir, "tests/mod_spec.lua", `
local M = require("mod")
describe("classify", function()
  it("high", function()
    assert.equals("high", M.classify(20))
    assert.equals("high", M.classify(15))
  end)
  it("mid", function()
    assert.equals("mid", M.classify(7))
  end)
end)
`)

	// minimal_init bootstraps plenary + points package.path at our
	// lua/ dir so require("mod") finds mod.lua. Cover mode passes this
	// as -u to nvim before the shim runs.
	minInit := writeE2EFile(t, dir, "minimal_init.lua", fmt.Sprintf(`
vim.opt.rtp:prepend(%q)
vim.opt.rtp:prepend(%q)
package.path = %q .. package.path
`, plenaryPath, dir, dir+"/lua/?.lua;"))

	// Real dependencies for cover.Executor. NeovimProvider hands back
	// the already-installed nvim path (E2E CI installs at job setup);
	// sandbox.NewFactory + realE2ERunner drive a real subprocess.
	// Named `executor` (not `exec`) to avoid shadowing the os/exec
	// package import in this file.
	executor := cover.NewExecutor(
		&realNvimProvider{path: nvimPath},
		sandbox.NewFactory(),
		realE2ERunner{},
		domain.Platform{OS: "linux", Arch: "amd64"},
	)

	cov, err := executor.Run(context.Background(), cover.Opts{
		Mode:                  cover.RunnerPlenaryBusted,
		Dir:                   testDir,
		MinimalInit:           minInit,
		CoverageSources:       []string{srcFile},
		BranchInstrumentation: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Assert on the returned CoverageData exactly as the run-mode E2E
	// does: 2 branches (if + elseif), 4 arms total, taken counts
	// matching runtime hits. If any of these fail, the failure mode
	// is either "shim didn't emit rewritten sources" or "attribution
	// didn't run" — both regressions the wire-integration test would
	// have missed.
	file := cov.FileByPath(srcFile)
	if file == nil {
		t.Fatalf("source file not in coverage: paths=%v", coveragePaths(cov))
	}
	if len(file.Branches) != 2 {
		t.Fatalf("expected 2 branches (if + elseif), got %d: %+v", len(file.Branches), file.Branches)
	}
	ifBranch := branchAtLine(t, file, 3)
	elseifBranch := branchAtLine(t, file, 5)
	if got := ifBranch.Arms[0].Taken; got != 2 {
		t.Errorf("if arm 0 (high) Taken = %d, want 2", got)
	}
	if got := elseifBranch.Arms[0].Taken; got != 1 {
		t.Errorf("elseif arm 0 (mid) Taken = %d, want 1", got)
	}
}

// TestBranchInstrumentation_TrueE2E_DofileShim verifies that files
// loaded via dofile (which bypasses package.loaders entirely) also
// get their rewritten source. Without the loadfile/dofile monkey-
// patch in coverage_hook.lua this test's counters would be empty:
// dofile calls C-level luaL_loadfile which reads on-disk bytes and
// never consults _neospec_rewritten_sources.
func TestBranchInstrumentation_TrueE2E_DofileShim(t *testing.T) {
	if os.Getenv("NEOSPEC_E2E") == "" {
		t.Skip("NEOSPEC_E2E env var not set — skipping true-e2e test")
	}
	nvimPath, err := exec.LookPath("nvim")
	if err != nil {
		t.Skipf("nvim not on PATH: %v", err)
	}

	dir := t.TempDir()

	// Same source shape as the require-path test.
	srcFile := writeE2EFile(t, dir, "mod.lua", `local M = {}
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

	// Load via dofile — the whole point of this test. Without the
	// shim, dofile reads mod.lua verbatim from disk, no counter fires,
	// BranchCounts stays empty. With the shim, dofile returns the
	// rewritten source and counters fire normally.
	testContent := fmt.Sprintf(`local M = dofile(%q)
describe("classify (dofile path)", function()
  it("high", function()
    assert.equals("high", M.classify(20))
    assert.equals("high", M.classify(15))
  end)
  it("mid", function()
    assert.equals("mid", M.classify(7))
  end)
end)
`, srcFile)
	testFile := writeE2EFile(t, dir, "test/mod_dofile_spec.lua", testContent)

	r := runner.NewWithDefaultSandbox(nvimPath, false, "", nil).
		WithCoverageSources([]string{srcFile}).
		WithSourceRewriter(func(paths []string) (map[string]string, any) {
			sources, injections := cover.RewriteAll(paths)
			return sources, injections
		})

	suite, cov, err := r.Run(context.Background(), []string{testFile})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(suite.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d — was rewritten source valid?\ntests: %+v",
			len(suite.Tests), suite.Tests)
	}
	for _, tr := range suite.Tests {
		if tr.Status != domain.StatusPass {
			t.Errorf("test %s: status=%v error=%s", tr.Name, tr.Status, tr.Error)
		}
	}

	injections, ok := r.Injections().([]cover.Injection)
	if !ok || len(injections) != 3 {
		t.Fatalf("Injections = %v (%T), want []cover.Injection of len 3",
			r.Injections(), r.Injections())
	}
	counts := r.BranchCounts()
	if len(counts) == 0 {
		t.Fatal("BranchCounts empty — dofile shim did not serve rewritten source; instrumentation silently bypassed")
	}
	assertCount(t, counts, injections[0].BranchID, 2, "high (then arm, dofile-loaded)")
	assertCount(t, counts, injections[1].BranchID, 1, "mid (elseif arm, dofile-loaded)")

	cover.PopulateBranches(cov)
	cover.ApplyBranchCounters(cov, injections, cov.BranchCounters)

	file := cov.FileByPath(srcFile)
	if file == nil {
		t.Fatalf("source file not in coverage: paths=%v", coveragePaths(cov))
	}
	ifBranch := branchAtLine(t, file, 3)
	elseifBranch := branchAtLine(t, file, 5)
	if got := ifBranch.Arms[0].Taken; got != 2 {
		t.Errorf("if arm 0 (high) Taken = %d, want 2", got)
	}
	if got := elseifBranch.Arms[0].Taken; got != 1 {
		t.Errorf("elseif arm 0 (mid) Taken = %d, want 1", got)
	}
}
