package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// errorSandboxFactory is a SandboxFactory that always returns an error.
type errorSandboxFactory struct{}

func (f *errorSandboxFactory) Create(_ context.Context) (ports.Sandbox, error) {
	return nil, fmt.Errorf("simulated sandbox creation failure")
}

// fakeCommandRunner is a CommandRunner that returns a canned stdout payload
// without invoking any real subprocess. Used to test runOne success/error paths.
type fakeCommandRunner struct {
	stdout []byte
	err    error
}

func (f *fakeCommandRunner) Run(_ context.Context, _ []string, _ string, _ ...string) ([]byte, []byte, error) {
	return f.stdout, nil, f.err
}

func TestNew(t *testing.T) {
	r := New("/usr/bin/nvim", sandbox.NewFactory(), realCommandRunner{}, false, "")
	if r == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewWithDefaultSandbox(t *testing.T) {
	r := NewWithDefaultSandbox("/usr/bin/nvim", true, "")
	if r == nil {
		t.Fatal("NewWithDefaultSandbox() returned nil")
	}
}

func TestRunner_Run_EmptyFiles(t *testing.T) {
	r := New("/usr/bin/nvim", sandbox.NewFactory(), realCommandRunner{}, false, "")
	suite, cov, err := r.Run(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Run() with empty files error: %v", err)
	}
	if suite == nil {
		t.Fatal("Run() returned nil suite")
	}
	if cov == nil {
		t.Fatal("Run() returned nil cov")
	}
	if len(suite.Tests) != 0 {
		t.Errorf("expected 0 tests, got %d", len(suite.Tests))
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		input string
		want  domain.TestStatus
	}{
		{"pass", domain.StatusPass},
		{"fail", domain.StatusFail},
		{"skip", domain.StatusSkip},
		{"error", domain.StatusError},
		{"unknown", domain.StatusError}, // default → error
		{"", domain.StatusError},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := parseStatus(tc.input); got != tc.want {
				t.Errorf("parseStatus(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseOutput_ValidJSON(t *testing.T) {
	data := runOutput{
		Tests: []testJSON{
			{Name: "my > test", Status: "pass", DurationMs: 12.5},
			{Name: "my > fail", Status: "fail", Error: "assertion failed"},
			{Name: "my > skip", Status: "skip"},
		},
		Coverage: []coverageJSON{
			{Path: "lua/mod.lua", Lines: map[string]int{"1": 3, "5": 0}},
		},
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	suite, cov, err := parseOutput(raw)
	if err != nil {
		t.Fatalf("parseOutput() error: %v", err)
	}

	if len(suite.Tests) != 3 {
		t.Fatalf("expected 3 tests, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Name != "my > test" {
		t.Errorf("test[0].Name = %q, want %q", suite.Tests[0].Name, "my > test")
	}
	if suite.Tests[0].Status != domain.StatusPass {
		t.Errorf("test[0].Status = %v, want pass", suite.Tests[0].Status)
	}
	if suite.Tests[1].Status != domain.StatusFail {
		t.Errorf("test[1].Status = %v, want fail", suite.Tests[1].Status)
	}
	if suite.Tests[1].Error != "assertion failed" {
		t.Errorf("test[1].Error = %q, want %q", suite.Tests[1].Error, "assertion failed")
	}

	if len(cov.Files) != 1 {
		t.Fatalf("expected 1 coverage file, got %d", len(cov.Files))
	}
	if cov.Files[0].Path != "lua/mod.lua" {
		t.Errorf("coverage file path = %q, want %q", cov.Files[0].Path, "lua/mod.lua")
	}
	if cov.Files[0].Lines[1] != 3 {
		t.Errorf("coverage line 1 = %d, want 3", cov.Files[0].Lines[1])
	}
}

func TestParseOutput_InvalidJSON(t *testing.T) {
	_, _, err := parseOutput([]byte("not json"))
	if err == nil {
		t.Error("parseOutput() expected error for invalid JSON")
	}
}

func TestRunner_Discover_Method(t *testing.T) {
	r := New("/usr/bin/nvim", sandbox.NewFactory(), realCommandRunner{}, false, "")
	files, err := r.Discover(context.Background(), []string{"/nonexistent/**"})
	if err != nil {
		t.Fatalf("Runner.Discover() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

// TestRunner_Run_BadNvimPath tests that when the nvim binary does not exist,
// Run records the failure as a StatusError test result rather than returning
// an error itself. The verbose sub-case also exercises the -V3 args prepend.
func TestRunner_Run_BadNvimPath(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
	}{
		{"non-verbose", false},
		{"verbose prepends -V3", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := New("/nonexistent/nvim-binary-that-does-not-exist", sandbox.NewFactory(), realCommandRunner{}, tc.verbose, "")
			testFile := createTempLuaFile(t)

			suite, cov, err := r.Run(context.Background(), []string{testFile})
			if err != nil {
				t.Fatalf("Run() should not return error (errors are recorded in suite): %v", err)
			}
			if suite == nil {
				t.Fatal("Run() returned nil suite")
			}
			if cov == nil {
				t.Fatal("Run() returned nil cov")
			}
			// The failed nvim execution is recorded as a test error, not a Run error.
			if len(suite.Tests) != 1 {
				t.Errorf("expected 1 error test result, got %d", len(suite.Tests))
			}
			if suite.Tests[0].Status != domain.StatusError {
				t.Errorf("expected StatusError, got %v", suite.Tests[0].Status)
			}
		})
	}
}

// TestRunner_Run_SandboxError covers the runOne sandbox-creation failure path.
// Run() records the error as a StatusError test result rather than returning it.
func TestRunner_Run_SandboxError(t *testing.T) {
	r := New("/usr/bin/nvim", &errorSandboxFactory{}, realCommandRunner{}, false, "")
	testFile := createTempLuaFile(t)

	// Coverage is not asserted here — this test focuses on the error recording path.
	suite, _, err := r.Run(context.Background(), []string{testFile})
	if err != nil {
		t.Fatalf("Run() should not return error (sandbox errors are recorded in suite): %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test result, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Status != domain.StatusError {
		t.Errorf("expected StatusError, got %v", suite.Tests[0].Status)
	}
}

// TestRunOne_FakeCommandRunner tests the runOne success path using a fake
// command runner that returns valid JSON, exercising parseOutput without nvim.
func TestRunOne_FakeCommandRunner(t *testing.T) {
	output := runOutput{
		Tests: []testJSON{{Name: "my > test", Status: "pass", DurationMs: 1.0}},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	r := New("/nvim", sandbox.NewFactory(), &fakeCommandRunner{stdout: raw}, false, "")
	testFile := createTempLuaFile(t)

	suite, cov, err := r.runOne(context.Background(), testFile)
	if err != nil {
		t.Fatalf("runOne() unexpected error: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Status != domain.StatusPass {
		t.Errorf("expected StatusPass, got %v", suite.Tests[0].Status)
	}
	if cov == nil {
		t.Fatal("runOne() returned nil cov")
	}
}

// TestRunOne_FakeCommandRunner_Error tests the runOne error path when the
// command runner returns a non-zero exit.
func TestRunOne_FakeCommandRunner_Error(t *testing.T) {
	r := New("/nvim", sandbox.NewFactory(), &fakeCommandRunner{err: fmt.Errorf("exit status 1")}, false, "")
	testFile := createTempLuaFile(t)

	_, _, err := r.runOne(context.Background(), testFile)
	if err == nil {
		t.Fatal("runOne() expected error, got nil")
	}
}

func createTempLuaFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.lua")
	if err := os.WriteFile(path, []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// fakeSandbox is a Sandbox that returns a caller-controlled Dir value.
// Used to inject a nonexistent directory path and trigger os.WriteFile failures.
type fakeSandbox struct {
	dir string
}

func (s *fakeSandbox) Env() []string { return nil }
func (s *fakeSandbox) Dir() string   { return s.dir }
func (s *fakeSandbox) Close() error  { return nil }

// fakeSandboxFactory always returns the provided fakeSandbox.
type fakeSandboxFactory struct {
	sb ports.Sandbox
}

func (f *fakeSandboxFactory) Create(_ context.Context) (ports.Sandbox, error) {
	return f.sb, nil
}

// TestRunner_Run_Success tests the success path of Run where runOne succeeds
// and results are appended to the suite and coverage. This covers the
// suite.Tests append and cov.Files append branches.
func TestRunner_Run_Success(t *testing.T) {
	output := runOutput{
		Tests:    []testJSON{{Name: "my > test", Status: "pass", DurationMs: 1.0}},
		Coverage: []coverageJSON{{Path: "lua/mod.lua", Lines: map[string]int{"1": 3}}},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	r := New("/nvim", sandbox.NewFactory(), &fakeCommandRunner{stdout: raw}, false, "")
	testFile := createTempLuaFile(t)

	suite, cov, err := r.Run(context.Background(), []string{testFile})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Status != domain.StatusPass {
		t.Errorf("expected StatusPass, got %v", suite.Tests[0].Status)
	}
	if len(cov.Files) != 1 {
		t.Errorf("expected 1 coverage file, got %d", len(cov.Files))
	}
}

// TestRunOne_WriteFileError tests the os.WriteFile error path in runOne.
// A sandbox that returns a nonexistent directory causes the shim write to fail.
func TestRunOne_WriteFileError(t *testing.T) {
	// Point the sandbox dir at a path whose parent does not exist so
	// os.WriteFile("…/neospec_run.lua") fails.
	nonexistentDir := filepath.Join(t.TempDir(), "deeply", "nonexistent", "dir")
	sb := &fakeSandbox{dir: nonexistentDir}
	r := New("/nvim", &fakeSandboxFactory{sb: sb}, &fakeCommandRunner{}, false, "")
	testFile := createTempLuaFile(t)

	_, _, err := r.runOne(context.Background(), testFile)
	if err == nil {
		t.Fatal("runOne() expected error when shim WriteFile fails, got nil")
	}
}

// TestRunner_Run_MultipleFiles verifies that Run aggregates results and coverage
// from all files in the list, including files processed after the first one.
// This is the critical invariant that must hold under parallel execution.
func TestRunner_Run_MultipleFiles(t *testing.T) {
	output := runOutput{
		Tests:    []testJSON{{Name: "spec > passes", Status: "pass", DurationMs: 1.0}},
		Coverage: []coverageJSON{{Path: "lua/mod.lua", Lines: map[string]int{"1": 2}}},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	r := New("/nvim", sandbox.NewFactory(), &fakeCommandRunner{stdout: raw}, false, "")
	files := []string{
		createTempLuaFile(t),
		createTempLuaFile(t),
		createTempLuaFile(t),
	}

	suite, cov, err := r.Run(context.Background(), files)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(suite.Tests) != 3 {
		t.Errorf("expected 3 tests (one per file), got %d", len(suite.Tests))
	}
	if len(cov.Files) != 3 {
		t.Errorf("expected 3 coverage files (one per file), got %d", len(cov.Files))
	}
}

// TestRunner_Run_MultipleFiles_PartialError verifies that an error in one file
// does not suppress results from the other files — all files are executed
// regardless of individual failures.
func TestRunner_Run_MultipleFiles_PartialError(t *testing.T) {
	goodOutput := runOutput{
		Tests: []testJSON{{Name: "spec > passes", Status: "pass", DurationMs: 1.0}},
	}
	goodRaw, err := json.Marshal(goodOutput)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// One runner that succeeds, one that errors — use the error sandbox path.
	// Inject a failing sandbox for one file via a counting factory.
	// Use the real factory for successes and error factory for the second call.
	successFactory := sandbox.NewFactory()
	errorFactory := &errorSandboxFactory{}
	mixedFactory := &countingSandboxFactory{
		factories: []ports.SandboxFactory{successFactory, errorFactory, successFactory},
	}
	r := New("/nvim", mixedFactory, &fakeCommandRunner{stdout: goodRaw}, false, "")
	files := []string{
		createTempLuaFile(t),
		createTempLuaFile(t),
		createTempLuaFile(t),
	}

	suite, _, err := r.Run(context.Background(), files)
	if err != nil {
		t.Fatalf("Run() should not return error — per-file errors are recorded in suite")
	}
	// 2 success tests + 1 error test result = 3 total
	if len(suite.Tests) != 3 {
		t.Errorf("expected 3 test results, got %d", len(suite.Tests))
	}
	var errorCount int
	for _, test := range suite.Tests {
		if test.Status == domain.StatusError {
			errorCount++
		}
	}
	if errorCount != 1 {
		t.Errorf("expected exactly 1 error result, got %d", errorCount)
	}
}

// countingSandboxFactory cycles through a list of SandboxFactory implementations
// so each Create() call uses the next factory in the list. callCount is an
// atomic int so the factory is safe to use from concurrent goroutines.
type countingSandboxFactory struct {
	factories []ports.SandboxFactory
	callCount atomic.Int64
}

func (f *countingSandboxFactory) Create(ctx context.Context) (ports.Sandbox, error) {
	i := int(f.callCount.Add(1)-1) % len(f.factories)
	return f.factories[i].Create(ctx)
}

// TestRunner_Run_ContextCancelled verifies that Run propagates a cancelled
// context as a non-nil error so callers can distinguish "the run was aborted"
// from "all test files failed normally".
//
// NOTE: the worker's ctx.Err() check in executor.go is non-atomic — a worker
// may pick up a job and call runOne before observing the cancellation. The only
// guaranteed contract is that Run returns context.Canceled. Asserting
// suite.Tests == 0 would be flaky under concurrent scheduling.
func TestRunner_Run_ContextCancelled(t *testing.T) {
	output := runOutput{
		Tests: []testJSON{{Name: "spec > passes", Status: "pass", DurationMs: 1.0}},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run is called

	r := New("/nvim", sandbox.NewFactory(), &fakeCommandRunner{stdout: raw}, false, "")
	files := []string{createTempLuaFile(t), createTempLuaFile(t)}

	_, _, err = r.Run(ctx, files)
	if err == nil {
		t.Error("Run() should return non-nil error when context is already cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// closingErrorSandbox is a fakeSandbox whose Close() always returns an error.
// Used to verify that runOne propagates sandbox cleanup failures.
type closingErrorSandbox struct {
	fakeSandbox
}

func (s *closingErrorSandbox) Close() error { return fmt.Errorf("simulated close failure") }

// TestRunOne_CloseError verifies that a non-nil error from sb.Close() is
// propagated by runOne so that temp-dir leaks surface as visible failures
// rather than being silently discarded.
func TestRunOne_CloseError(t *testing.T) {
	output := runOutput{
		Tests: []testJSON{{Name: "a > test", Status: "pass", DurationMs: 1.0}},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	sb := &closingErrorSandbox{fakeSandbox{dir: t.TempDir()}}
	r := New("/nvim", &fakeSandboxFactory{sb: sb}, &fakeCommandRunner{stdout: raw}, false, "")

	_, _, err = r.runOne(context.Background(), createTempLuaFile(t))
	if err == nil {
		t.Fatal("runOne() expected error from Close(), got nil")
	}
	if !strings.Contains(err.Error(), "closing sandbox") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "closing sandbox")
	}
}

// TestRunner_Run_CloseError verifies that a sandbox Close() error is recorded
// as a StatusError test result in Run's aggregated output, not silently
// discarded. This exercises the path where runOne returns an error (via the
// named-return close error) and Run records it in the suite.
func TestRunner_Run_CloseError(t *testing.T) {
	output := runOutput{
		Tests: []testJSON{{Name: "a > test", Status: "pass", DurationMs: 1.0}},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	sb := &closingErrorSandbox{fakeSandbox{dir: t.TempDir()}}
	r := New("/nvim", &fakeSandboxFactory{sb: sb}, &fakeCommandRunner{stdout: raw}, false, "")

	suite, _, err := r.Run(context.Background(), []string{createTempLuaFile(t)})
	if err != nil {
		t.Fatalf("Run() should not return error (errors are recorded in suite): %v", err)
	}
	if len(suite.Tests) != 1 {
		t.Fatalf("expected 1 test result, got %d", len(suite.Tests))
	}
	if suite.Tests[0].Status != domain.StatusError {
		t.Errorf("expected StatusError, got %v", suite.Tests[0].Status)
	}
	if !strings.Contains(suite.Tests[0].Error, "closing sandbox") {
		t.Errorf("error = %q, want to contain %q", suite.Tests[0].Error, "closing sandbox")
	}
}

func TestParseOutput_EmptyLinesArray(t *testing.T) {
	raw := []byte(`{"tests":[],"coverage":[{"path":"lua/mod.lua","lines":[]}]}`)
	_, cov, err := parseOutput(raw)
	if err != nil {
		t.Fatalf("parseOutput() error on empty lines array: %v", err)
	}
	if len(cov.Files) != 0 {
		t.Errorf("expected 0 coverage files (empty lines entry skipped), got %d", len(cov.Files))
	}
}

func TestParseOutput_InvalidLineNumber(t *testing.T) {
	// A non-numeric line key indicates corrupted harness output; parseOutput
	// must return an error. Use a single bad key so the test is unconditional
	// regardless of map iteration order.
	raw := []byte(`{"tests":[],"coverage":[{"path":"lua/mod.lua","lines":{"notanumber":1}}]}`)
	_, _, err := parseOutput(raw)
	if err == nil {
		t.Error("parseOutput() expected error for non-numeric line key, got nil")
	}
}

// TestParseOutput_PartialNumericLineKey verifies that parseOutput rejects a line
// key like "42abc" — a partial-numeric string that fmt.Sscan would silently
// truncate to 42 but strconv.Atoi correctly rejects. Corrupted harness output
// with such keys must surface as an error, not silently record the wrong line.
func TestParseOutput_PartialNumericLineKey(t *testing.T) {
	raw := []byte(`{"tests":[],"coverage":[{"path":"lua/mod.lua","lines":{"42abc":1}}]}`)
	_, _, err := parseOutput(raw)
	if err == nil {
		t.Error("parseOutput() expected error for partial-numeric line key \"42abc\", got nil")
	}
}

// TestParseOutput_LuaReporterError verifies that parseOutput returns an error
// when the Lua reporter emits an "error" field in its JSON output. reporter.lua's
// pcall guard writes {"tests":[],"coverage":[],"error":"..."} when the
// serialisation fails; without this check the Go consumer sees a successful
// run with zero tests instead of a surfaced error.
func TestParseOutput_LuaReporterError(t *testing.T) {
	raw := []byte(`{"tests":[],"coverage":[],"error":"pcall failed: attempt to index nil"}`)
	_, _, err := parseOutput(raw)
	if err == nil {
		t.Error("parseOutput() expected error when Lua reporter emits an error field, got nil")
	}
}
