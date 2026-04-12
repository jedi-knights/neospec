package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

func TestNew(t *testing.T) {
	r := New("/usr/bin/nvim", sandbox.NewFactory(), false)
	if r == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewWithDefaultSandbox(t *testing.T) {
	r := NewWithDefaultSandbox("/usr/bin/nvim", true)
	if r == nil {
		t.Fatal("NewWithDefaultSandbox() returned nil")
	}
}

func TestRunner_Run_EmptyFiles(t *testing.T) {
	r := New("/usr/bin/nvim", sandbox.NewFactory(), false)
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
		tc := tc
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
	raw, _ := json.Marshal(data)

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
	r := New("/usr/bin/nvim", sandbox.NewFactory(), false)
	files, err := r.Discover(context.Background(), []string{"/nonexistent/**"})
	if err != nil {
		t.Fatalf("Runner.Discover() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

// TestRunner_Run_BadNvimPath tests the error path in Run when the nvim binary
// does not exist. runOne returns an error which Run records as a test failure.
func TestRunner_Run_BadNvimPath(t *testing.T) {
	r := New("/nonexistent/nvim-binary-that-does-not-exist", sandbox.NewFactory(), false)

	// Create a real file to pass as a test file.
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
}

// TestRunner_Run_BadNvimPath_Verbose tests the verbose path in runOne.
func TestRunner_Run_BadNvimPath_Verbose(t *testing.T) {
	r := New("/nonexistent/nvim-binary-verbose", sandbox.NewFactory(), true)
	testFile := createTempLuaFile(t)
	// Should not panic; verbose just prepends -V3 to args.
	r.Run(context.Background(), []string{testFile}) //nolint:errcheck
}

// TestRunner_Run_SandboxError covers the runOne sandbox-creation failure path.
// Run() records the error as a StatusError test result rather than returning it.
func TestRunner_Run_SandboxError(t *testing.T) {
	r := New("/usr/bin/nvim", &errorSandboxFactory{}, false)
	testFile := createTempLuaFile(t)

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

func createTempLuaFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/spec.lua"
	if err := os.WriteFile(path, []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestParseOutput_InvalidLineNumber(t *testing.T) {
	// If a line key is not a valid integer, it should be skipped rather than crash.
	data := runOutput{
		Coverage: []coverageJSON{
			{Path: "lua/mod.lua", Lines: map[string]int{"notanumber": 1, "2": 5}},
		},
	}
	raw, _ := json.Marshal(data)

	_, cov, err := parseOutput(raw)
	if err != nil {
		t.Fatalf("parseOutput() unexpected error: %v", err)
	}
	if cov.Files[0].Lines[2] != 5 {
		t.Errorf("expected line 2 = 5, got %d", cov.Files[0].Lines[2])
	}
}
