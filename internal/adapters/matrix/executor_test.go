package matrix

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// Fake NeovimProvider that returns a pre-configured path per version tag,
// or an error if the tag is registered as a failure.
type fakeNeovim struct {
	paths  map[string]string
	errs   map[string]error
	called []domain.Version
}

func (f *fakeNeovim) Ensure(_ context.Context, v domain.Version, _ domain.Platform) (string, error) {
	f.called = append(f.called, v)
	if err, ok := f.errs[v.Tag]; ok {
		return "", err
	}
	return f.paths[v.Tag], nil
}

// Fake SandboxFactory that hands out predictable in-memory sandboxes.
type fakeSandboxFactory struct {
	created []string // tracks the sandbox Dir values handed out
	err     error    // if non-nil, every Create returns this error
}

func (f *fakeSandboxFactory) Create(_ context.Context) (ports.Sandbox, error) {
	if f.err != nil {
		return nil, f.err
	}
	sb := &fakeSandbox{dir: filepath.Join(os.TempDir(), "neospec-fake-sandbox")}
	f.created = append(f.created, sb.dir)
	return sb, nil
}

type fakeSandbox struct {
	dir      string
	closed   bool
	closeErr error
}

func (s *fakeSandbox) Env() []string {
	return []string{"XDG_CONFIG_HOME=" + s.dir + "/config", "HOME=" + s.dir}
}
func (s *fakeSandbox) Dir() string  { return s.dir }
func (s *fakeSandbox) Close() error { s.closed = true; return s.closeErr }

// Fake CommandRunner records every invocation and returns canned outputs per
// binary path (keyed by the executable name), or the fallback if no key match.
type fakeCommandRunner struct {
	responses map[string]cmdResponse
	fallback  cmdResponse
	calls     []cmdCall
}

type cmdResponse struct {
	stdout []byte
	stderr []byte
	err    error
}

type cmdCall struct {
	env  []string
	name string
	args []string
}

func (f *fakeCommandRunner) Run(_ context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, cmdCall{env: env, name: name, args: args})
	if r, ok := f.responses[name]; ok {
		return r.stdout, r.stderr, r.err
	}
	return f.fallback.stdout, f.fallback.stderr, f.fallback.err
}

var _ ports.NeovimProvider = (*fakeNeovim)(nil)
var _ ports.SandboxFactory = (*fakeSandboxFactory)(nil)
var _ ports.CommandRunner = (*fakeCommandRunner)(nil)

func newTestExecutor(t *testing.T) (*Executor, *fakeNeovim, *fakeSandboxFactory, *fakeCommandRunner) {
	t.Helper()
	n := &fakeNeovim{
		paths: map[string]string{
			"stable":  "/tmp/nvim-stable/bin/nvim",
			"nightly": "/tmp/nvim-nightly/bin/nvim",
		},
		errs: map[string]error{},
	}
	sf := &fakeSandboxFactory{}
	cr := &fakeCommandRunner{responses: map[string]cmdResponse{}}
	e := NewExecutor(n, sf, cr, domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64})
	return e, n, sf, cr
}

func TestExecutor_Run_EmptyCommand(t *testing.T) {
	e, _, _, _ := newTestExecutor(t)
	_, err := e.Run(context.Background(), nil, []domain.Version{{Tag: "stable"}})
	if err == nil || !strings.Contains(err.Error(), "command is empty") {
		t.Errorf("want empty-command error, got: %v", err)
	}
}

func TestExecutor_Run_EmptyVersions(t *testing.T) {
	e, _, _, _ := newTestExecutor(t)
	_, err := e.Run(context.Background(), []string{"nvim", "--version"}, nil)
	if err == nil || !strings.Contains(err.Error(), "no versions") {
		t.Errorf("want no-versions error, got: %v", err)
	}
}

func TestExecutor_Run_SingleVersionSuccess(t *testing.T) {
	e, _, _, cr := newTestExecutor(t)
	cr.fallback = cmdResponse{stdout: []byte("NVIM v0.10.4\n")}

	m, err := e.Run(context.Background(), []string{"nvim", "--version"}, []domain.Version{{Tag: "stable"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(m.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(m.Results))
	}
	r := m.Results[0]
	if !r.Passed() {
		t.Errorf("want passed, got %+v", r)
	}
	if string(r.Stdout) != "NVIM v0.10.4\n" {
		t.Errorf("stdout = %q, want \"NVIM v0.10.4\\n\"", string(r.Stdout))
	}
	if !m.Passed() {
		t.Errorf("matrix.Passed() = false, want true")
	}
}

func TestExecutor_Run_MultiVersionMixedOutcome(t *testing.T) {
	// Stateful stub returns one response per call, so we can model different
	// outcomes across the two versions.
	failErr := makeExitError(t, 1)
	stub := &stubRunner{
		outputs: []cmdResponse{
			{stdout: []byte("stable output\n")},
			{stdout: []byte("nightly stdout\n"), stderr: []byte("nightly stderr\n"), err: failErr},
		},
	}
	e := NewExecutor(
		&fakeNeovim{paths: map[string]string{"stable": "/tmp/a/bin/nvim", "nightly": "/tmp/b/bin/nvim"}},
		&fakeSandboxFactory{},
		stub,
		domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64},
	)

	m, err := e.Run(context.Background(), []string{"nvim", "--version"},
		[]domain.Version{{Tag: "stable"}, {Tag: "nightly"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(m.Results) != 2 {
		t.Fatalf("want 2 results, got %d", len(m.Results))
	}
	if !m.Results[0].Passed() {
		t.Errorf("stable want passed, got %+v", m.Results[0])
	}
	if m.Results[1].Passed() {
		t.Errorf("nightly want failed, got %+v", m.Results[1])
	}
	if m.Results[1].ExitCode == 0 {
		t.Errorf("nightly ExitCode = 0, want non-zero (from ExitError)")
	}
	if m.PassedCount() != 1 || m.FailedCount() != 1 {
		t.Errorf("counts = %d/%d, want 1/1", m.PassedCount(), m.FailedCount())
	}
}

func TestExecutor_Run_NeovimProvisionError(t *testing.T) {
	e, n, _, _ := newTestExecutor(t)
	n.errs["nightly"] = errors.New("download timeout")

	m, err := e.Run(context.Background(), []string{"nvim", "--version"},
		[]domain.Version{{Tag: "stable"}, {Tag: "nightly"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !m.Results[0].Passed() {
		t.Errorf("stable want passed, got %+v", m.Results[0])
	}
	r := m.Results[1]
	if r.Err == nil {
		t.Errorf("nightly Err = nil, want non-nil")
	}
	if r.Passed() {
		t.Errorf("nightly Passed() = true, want false (provision error)")
	}
}

func TestExecutor_Run_SandboxError(t *testing.T) {
	n := &fakeNeovim{paths: map[string]string{"stable": "/tmp/a/bin/nvim"}}
	sf := &fakeSandboxFactory{err: errors.New("no space left")}
	cr := &fakeCommandRunner{}
	e := NewExecutor(n, sf, cr, domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64})

	m, err := e.Run(context.Background(), []string{"nvim"}, []domain.Version{{Tag: "stable"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	r := m.Results[0]
	if r.Err == nil || !strings.Contains(r.Err.Error(), "no space left") {
		t.Errorf("want sandbox error wrapped, got %v", r.Err)
	}
}

func TestVersionEnv_PrependsNvimDir(t *testing.T) {
	env := versionEnv([]string{"XDG_CONFIG_HOME=/x"}, "/tmp/nvim-stable/bin/nvim")

	if len(env) != 2 {
		t.Fatalf("want 2 env entries, got %d: %v", len(env), env)
	}
	if env[0] != "XDG_CONFIG_HOME=/x" {
		t.Errorf("sandbox env not preserved as first entry: %q", env[0])
	}
	if !strings.HasPrefix(env[1], "PATH=/tmp/nvim-stable/bin") {
		t.Errorf("PATH entry does not lead with the nvim dir: %q", env[1])
	}
}

func TestExitCodeFrom_Nil(t *testing.T) {
	if got := exitCodeFrom(nil); got != 0 {
		t.Errorf("exitCodeFrom(nil) = %d, want 0", got)
	}
}

func TestExitCodeFrom_ExitError(t *testing.T) {
	err := makeExitError(t, 42)
	if got := exitCodeFrom(err); got != 42 {
		t.Errorf("exitCodeFrom(ExitError 42) = %d, want 42", got)
	}
}

func TestExitCodeFrom_NonExitError(t *testing.T) {
	if got := exitCodeFrom(errors.New("random")); got != 0 {
		t.Errorf("exitCodeFrom(non-exit) = %d, want 0", got)
	}
}

func TestWrapUnknownRunErr(t *testing.T) {
	if wrapUnknownRunErr(nil) != nil {
		t.Errorf("nil in, want nil out")
	}
	exitErr := makeExitError(t, 1)
	if wrapUnknownRunErr(exitErr) != nil {
		t.Errorf("ExitError in, want nil out (exit is carried in ExitCode field)")
	}
	other := errors.New("executable not found")
	if wrapUnknownRunErr(other) != other {
		t.Errorf("non-exit error should pass through unchanged")
	}
}

// stubRunner returns a scripted sequence of responses, one per call.
type stubRunner struct {
	outputs []cmdResponse
	n       int
}

func (s *stubRunner) Run(_ context.Context, _ []string, _ string, _ ...string) ([]byte, []byte, error) {
	if s.n >= len(s.outputs) {
		return nil, nil, errors.New("stubRunner exhausted")
	}
	r := s.outputs[s.n]
	s.n++
	return r.stdout, r.stderr, r.err
}

// makeExitError produces a real *exec.ExitError with the specified exit code
// by running a subprocess that exits with that code. This is the only reliable
// way to construct an ExitError since its fields are unexported.
func makeExitError(t *testing.T, code int) error {
	t.Helper()
	// `sh -c 'exit N'` is available on every unix runner; skip on windows.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available; ExitError construction unavailable")
	}
	cmd := exec.Command("sh", "-c", "exit "+itoa(code))
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected sh exit %d to produce an error", code)
	}
	return err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
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
