package cover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

type fakeNeovim struct {
	path string
	err  error
}

func (f *fakeNeovim) Ensure(_ context.Context, _ domain.Version, _ domain.Platform) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.path, nil
}

type fakeSandboxFactory struct {
	dir string
	err error
}

func (f *fakeSandboxFactory) Create(_ context.Context) (ports.Sandbox, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fakeSandbox{dir: f.dir}, nil
}

type fakeSandbox struct{ dir string }

func (s *fakeSandbox) Env() []string { return []string{"HOME=" + s.dir} }
func (s *fakeSandbox) Dir() string   { return s.dir }
func (s *fakeSandbox) Close() error  { return nil }

// fakeRunner writes the caller-specified coverage JSON to the output file the
// executor derives from the sandbox dir, then returns the configured error.
// This models what the real Neovim + reporter would do after running.
type fakeRunner struct {
	writeJSON  []byte // written to <sandbox>/neospec_cover_output.json on Run
	returnErr  error  // returned from Run
	stderrData []byte
	seenEnv    []string
	seenArgs   []string
}

func (f *fakeRunner) Run(_ context.Context, env []string, _ string, args ...string) ([]byte, []byte, error) {
	f.seenEnv = env
	f.seenArgs = args
	if f.writeJSON != nil {
		// Derive the output file path the executor picks — same convention.
		// The sandbox dir is the parent dir of the shim path in args.
		var dir string
		for i, a := range args {
			if a == "-l" && i+1 < len(args) {
				dir = filepath.Dir(args[i+1])
				break
			}
		}
		if dir == "" {
			// External mode has no -l; the env has HOME=<sandbox-dir>.
			for _, e := range env {
				if strings.HasPrefix(e, "HOME=") {
					dir = strings.TrimPrefix(e, "HOME=")
					break
				}
			}
		}
		if dir != "" {
			_ = os.WriteFile(filepath.Join(dir, "neospec_cover_output.json"), f.writeJSON, 0o644)
		}
	}
	return nil, f.stderrData, f.returnErr
}

var _ ports.NeovimProvider = (*fakeNeovim)(nil)
var _ ports.SandboxFactory = (*fakeSandboxFactory)(nil)
var _ ports.CommandRunner = (*fakeRunner)(nil)

func newTestExecutor(t *testing.T, coverageJSON []byte, runnerErr error) (*Executor, *fakeRunner, string) {
	t.Helper()
	dir := t.TempDir()
	fr := &fakeRunner{writeJSON: coverageJSON, returnErr: runnerErr}
	e := NewExecutor(
		&fakeNeovim{path: "/tmp/nvim/bin/nvim"},
		&fakeSandboxFactory{dir: dir},
		fr,
		domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64},
	)
	return e, fr, dir
}

// A minimal valid coverage JSON emitted by the reporter shape.
const validCoverageJSON = `{"tests":[],"coverage":[{"path":"lua/foo.lua","lines":{"1":1,"2":3}}]}`

func TestExecutor_Run_PlenaryBustedHappyPath(t *testing.T) {
	e, fr, _ := newTestExecutor(t, []byte(validCoverageJSON), nil)

	cov, err := e.Run(context.Background(), Opts{
		Mode:        RunnerPlenaryBusted,
		Version:     domain.Version{Tag: "stable"},
		Dir:         "tests/",
		MinimalInit: "tests/minimal_init.vim",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cov == nil || len(cov.Files) != 1 {
		t.Fatalf("want 1 file of coverage, got %+v", cov)
	}
	if cov.Files[0].Path != "lua/foo.lua" {
		t.Errorf("path = %q, want lua/foo.lua", cov.Files[0].Path)
	}
	// Verify -u <init> and -l <shim> both landed in args
	joined := strings.Join(fr.seenArgs, " ")
	if !strings.Contains(joined, "-u tests/minimal_init.vim") {
		t.Errorf("args missing minimal-init: %q", joined)
	}
	if !strings.Contains(joined, "-l ") {
		t.Errorf("args missing shim -l: %q", joined)
	}
}

func TestExecutor_Run_MiniTest(t *testing.T) {
	e, _, _ := newTestExecutor(t, []byte(validCoverageJSON), nil)

	cov, err := e.Run(context.Background(), Opts{
		Mode:    RunnerMiniTest,
		Version: domain.Version{Tag: "stable"},
		Dir:     "tests/",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cov == nil {
		t.Fatalf("cov is nil")
	}
}

func TestExecutor_Run_DefaultsToStable(t *testing.T) {
	e, _, _ := newTestExecutor(t, []byte(validCoverageJSON), nil)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerPlenaryBusted, Dir: "tests/"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestExecutor_Run_WrappedRunnerFailStillReturnsCoverage(t *testing.T) {
	// Plenary/mini exit non-zero on test failure but the coverage reporter
	// still fires. Executor should surface both: the coverage data (if any)
	// and the runner error.
	e, _, _ := newTestExecutor(t, []byte(validCoverageJSON), errors.New("test suite failed"))
	cov, err := e.Run(context.Background(), Opts{Mode: RunnerPlenaryBusted, Dir: "tests/", Version: domain.Version{Tag: "stable"}})
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "test suite failed") {
		t.Errorf("error should surface wrapped-runner error: %v", err)
	}
	if cov == nil || len(cov.Files) == 0 {
		t.Errorf("coverage should be returned alongside runner error, got %+v", cov)
	}
}

func TestExecutor_Run_NoCoverageFileWritten(t *testing.T) {
	// Runner exits successfully but somehow doesn't produce the output file
	// (e.g., the reporter autocmd crashed). Should be reported as a clear
	// diagnostic, not a JSON parse error.
	e, _, _ := newTestExecutor(t, nil, nil)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerPlenaryBusted, Dir: "tests/", Version: domain.Version{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "output file not written") {
		t.Errorf("want output-not-written diagnostic, got: %v", err)
	}
}

func TestExecutor_Run_MalformedCoverageJSON(t *testing.T) {
	e, _, _ := newTestExecutor(t, []byte("{not valid json"), nil)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerPlenaryBusted, Dir: "tests/", Version: domain.Version{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "parsing cover output") {
		t.Errorf("want parse-error diagnostic, got: %v", err)
	}
}

func TestExecutor_Run_NeovimProvisionError(t *testing.T) {
	e := NewExecutor(
		&fakeNeovim{err: errors.New("download failed")},
		&fakeSandboxFactory{dir: t.TempDir()},
		&fakeRunner{},
		domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64},
	)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerPlenaryBusted, Dir: "tests/", Version: domain.Version{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Errorf("want provision error, got: %v", err)
	}
}

func TestExecutor_Run_SandboxError(t *testing.T) {
	e := NewExecutor(
		&fakeNeovim{path: "/tmp/nvim/bin/nvim"},
		&fakeSandboxFactory{err: errors.New("no space")},
		&fakeRunner{},
		domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64},
	)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerPlenaryBusted, Dir: "tests/", Version: domain.Version{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "no space") {
		t.Errorf("want sandbox error, got: %v", err)
	}
}

func TestExecutor_Run_ExternalNoCommand(t *testing.T) {
	e, _, _ := newTestExecutor(t, nil, nil)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerExternal, Version: domain.Version{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "requires --cmd") {
		t.Errorf("want --cmd required error, got: %v", err)
	}
}

func TestExecutor_Run_ExternalHappyPath(t *testing.T) {
	e, fr, _ := newTestExecutor(t, []byte(validCoverageJSON), nil)
	cov, err := e.Run(context.Background(), Opts{
		Mode:    RunnerExternal,
		Command: []string{"make", "test"},
		Version: domain.Version{Tag: "stable"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cov == nil {
		t.Fatalf("cov is nil")
	}
	// Verify NEOSPEC_COVER_HOOK and NEOSPEC_COVER_OUTPUT env vars set
	joined := strings.Join(fr.seenEnv, " ")
	if !strings.Contains(joined, "NEOSPEC_COVER_HOOK=") {
		t.Errorf("env missing NEOSPEC_COVER_HOOK: %q", joined)
	}
	if !strings.Contains(joined, "NEOSPEC_COVER_OUTPUT=") {
		t.Errorf("env missing NEOSPEC_COVER_OUTPUT: %q", joined)
	}
}

func TestExecutor_Run_UnknownMode(t *testing.T) {
	e, _, _ := newTestExecutor(t, nil, nil)
	_, err := e.Run(context.Background(), Opts{Mode: RunnerMode("bogus"), Version: domain.Version{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "unknown runner mode") {
		t.Errorf("want unknown-mode error, got: %v", err)
	}
}

func TestExecutor_Run_VerboseAddsV3(t *testing.T) {
	e, fr, _ := newTestExecutor(t, []byte(validCoverageJSON), nil)
	_, err := e.Run(context.Background(), Opts{
		Mode:    RunnerPlenaryBusted,
		Dir:     "tests/",
		Version: domain.Version{Tag: "stable"},
		Verbose: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fr.seenArgs) == 0 || fr.seenArgs[0] != "-V3" {
		t.Errorf("verbose should prepend -V3 to args, got: %v", fr.seenArgs)
	}
}

func TestExecutor_Run_ExternalWrappedFailStillReturnsCoverage(t *testing.T) {
	e, _, _ := newTestExecutor(t, []byte(validCoverageJSON), errors.New("make failed"))
	cov, err := e.Run(context.Background(), Opts{
		Mode:    RunnerExternal,
		Command: []string{"make", "test"},
		Version: domain.Version{Tag: "stable"},
	})
	if err == nil || !strings.Contains(err.Error(), "make failed") {
		t.Errorf("want wrapped-command error surfaced, got: %v", err)
	}
	if cov == nil || len(cov.Files) == 0 {
		t.Errorf("coverage should be returned alongside runner error, got %+v", cov)
	}
}

func TestExecutor_Run_ExternalWritesHookFile(t *testing.T) {
	e, _, dir := newTestExecutor(t, []byte(validCoverageJSON), nil)
	_, err := e.Run(context.Background(), Opts{
		Mode:    RunnerExternal,
		Command: []string{"make", "test"},
		Version: domain.Version{Tag: "stable"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Confirm the hook file was written to the sandbox
	hookPath := filepath.Join(dir, "neospec_cover_hook.lua")
	if _, err := os.Stat(hookPath); err != nil {
		t.Errorf("hook file not created at %s: %v", hookPath, err)
	}
}

// TestResolveGlobs_Empty pins the nil/empty short-circuit — no
// filesystem walk, no allocation, no error.
func TestResolveGlobs_Empty(t *testing.T) {
	if got := resolveGlobs(context.Background(), nil); got != nil {
		t.Errorf("resolveGlobs(nil) = %v, want nil", got)
	}
	if got := resolveGlobs(context.Background(), []string{}); got != nil {
		t.Errorf("resolveGlobs(empty) = %v, want nil", got)
	}
}

// TestResolveGlobs_ResolvesRealFiles happy path — writes a couple of
// files under a tempdir, resolves with a matching glob, verifies the
// absolute paths come back. Pins the "discover + absolutize" pipeline
// end-to-end without a real runner test.
func TestResolveGlobs_ResolvesRealFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.lua", "b.lua"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("--"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	got := resolveGlobs(context.Background(), []string{filepath.Join(dir, "*.lua")})
	if len(got) != 2 {
		t.Fatalf("resolveGlobs returned %d paths, want 2: %v", len(got), got)
	}
	for _, p := range got {
		if !filepath.IsAbs(p) {
			t.Errorf("path not absolute: %s", p)
		}
	}
}

// TestResolveGlobs_NoMatchIsNil pins that "glob matched nothing"
// degrades to nil rather than an empty slice — same shape as the
// nil-input case so downstream callers only need one check.
func TestResolveGlobs_NoMatchIsNil(t *testing.T) {
	dir := t.TempDir()
	got := resolveGlobs(context.Background(), []string{filepath.Join(dir, "nonexistent-*.lua")})
	if got != nil {
		t.Errorf("resolveGlobs(no-match) = %v, want nil", got)
	}
}

// TestExecutor_BranchInstrumentationDrivesFullPipeline verifies the
// end-to-end wiring the executor picks up when opts.BranchInstrumentation
// is set: real RewriteAll on the resolved source, real shim emission
// of rewritten bytes, and — critically — real PopulateBranches +
// ApplyBranchCounters called against the CoverageData that comes back.
// The scripted fakeRunner supplies canned reporter JSON that includes
// br_counts, standing in for what a real Neovim run would produce.
func TestExecutor_BranchInstrumentationDrivesFullPipeline(t *testing.T) {
	// Arrange: a source with three arms (if/elseif/else). RewriteAll
	// will produce 3 injections whose BranchIDs we can read back to
	// build a matching counter map.
	dir := t.TempDir()
	src := writeFileAbs(t, dir, "mod.lua", `local M = {}
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

	// Precompute what the executor's internal RewriteAll call will
	// produce, so the fake reporter JSON can reference the exact
	// BranchIDs the executor will attribute against.
	_, injs := RewriteAll([]string{src})
	if len(injs) != 3 {
		t.Fatalf("expected 3 injections (then + elseif + else), got %d", len(injs))
	}

	// Reporter JSON simulating a run where the "high" arm ran twice
	// and the "mid" arm ran once — matches the pattern the true-e2e
	// test in cover_test.go pins for run mode.
	reply := []byte(`{"tests":[],"coverage":[{"path":"` + src + `","lines":{"4":2,"6":1,"8":0}}],"br_counts":{"` +
		itoa(injs[0].BranchID) + `":2,"` + itoa(injs[1].BranchID) + `":1}}`)

	nvim := &fakeNeovim{path: "/fake/nvim"}
	sbFactory := &fakeSandboxFactory{dir: dir}
	runner := &fakeRunner{writeJSON: reply}

	// Act
	exec := NewExecutor(nvim, sbFactory, runner, domain.Platform{OS: "linux", Arch: "amd64"})
	cov, err := exec.Run(context.Background(), Opts{
		Mode:                  RunnerPlenaryBusted,
		Dir:                   "tests/",
		CoverageSources:       []string{src},
		BranchInstrumentation: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Assert — attribution ran, arms have the runtime counter values.
	// This mirrors the assertions the run-mode integration test pins
	// for the same source shape.
	if cov == nil || len(cov.Files) != 1 {
		t.Fatalf("expected 1 covered file, got: %+v", cov)
	}
	file := cov.Files[0]
	if len(file.Branches) != 2 {
		t.Fatalf("expected 2 branches (if + elseif), got %d", len(file.Branches))
	}
	// if-branch: arm 0 (then, high) taken 2× via counter attribution;
	// arm 1 (fallthrough to elseif body line 6) taken 1× via line-hit
	// derivation on line 6.
	ifBranch := branchAt(t, file, 3)
	if got := ifBranch.Arms[0].Taken; got != 2 {
		t.Errorf("if arm 0 (high) Taken = %d, want 2", got)
	}
	if got := ifBranch.Arms[1].Taken; got != 1 {
		t.Errorf("if arm 1 (fallthrough) Taken = %d, want 1", got)
	}
	// elseif-branch: arm 0 (then, mid) taken 1× via counter — this is
	// the arm the pre-#40 arm-index bug would have written to arm 1.
	elseifBranch := branchAt(t, file, 5)
	if got := elseifBranch.Arms[0].Taken; got != 1 {
		t.Errorf("elseif arm 0 (mid) Taken = %d, want 1", got)
	}
}

// writeFileAbs writes contents to dir/name and returns the absolute
// path. Convenient for tests that need to both write a fixture and
// look it up in a map keyed by absolute path.
func writeFileAbs(t *testing.T, dir, name, contents string) string {
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

// branchAt is a small helper to keep branch lookups in the test body
// readable — otherwise the arm assertions get lost in linear scans.
func branchAt(t *testing.T, file *domain.FileCoverage, line int) *domain.BranchCoverage {
	t.Helper()
	for i := range file.Branches {
		if file.Branches[i].Line == line {
			return &file.Branches[i]
		}
	}
	t.Fatalf("no branch at line %d", line)
	return nil
}

// itoa is a tiny stringer so the reporter-JSON literal above stays
// readable without importing strconv into this file for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
