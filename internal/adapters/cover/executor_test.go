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
