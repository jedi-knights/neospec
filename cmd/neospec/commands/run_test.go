package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/config"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// fakeNeovimProvider satisfies ports.NeovimProvider for tests.
type fakeNeovimProvider struct {
	path string
	err  error
}

func (f *fakeNeovimProvider) Ensure(_ context.Context, _ domain.Version, _ domain.Platform) (string, error) {
	return f.path, f.err
}

// fakeTestRunner satisfies ports.TestRunner for tests.
type fakeTestRunner struct {
	files       []string
	suite       *domain.SuiteResult
	cov         *domain.CoverageData
	discoverErr error
	runErr      error
}

func (f *fakeTestRunner) Discover(_ context.Context, _ []string) ([]string, error) {
	return f.files, f.discoverErr
}

func (f *fakeTestRunner) Run(_ context.Context, _ []string) (*domain.SuiteResult, *domain.CoverageData, error) {
	return f.suite, f.cov, f.runErr
}

// compile-time interface compliance checks
var _ ports.NeovimProvider = (*fakeNeovimProvider)(nil)
var _ ports.TestRunner = (*fakeTestRunner)(nil)

func TestCheckThreshold_BelowThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 80.0}
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "f.lua", Lines: map[int]int{1: 1, 2: 0, 3: 0, 4: 0}}, // 25%
		},
	}
	err := checkThreshold(cfg, cov)
	if err == nil {
		t.Error("checkThreshold() expected error when below threshold")
	}
}

func TestCheckThreshold_AboveThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 50.0}
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "f.lua", Lines: map[int]int{1: 1, 2: 1}}, // 100%
		},
	}
	if err := checkThreshold(cfg, cov); err != nil {
		t.Errorf("checkThreshold() unexpected error: %v", err)
	}
}

func TestCheckThreshold_ZeroThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 0}
	cov := &domain.CoverageData{} // 0% coverage
	if err := checkThreshold(cfg, cov); err != nil {
		t.Errorf("checkThreshold() with zero threshold should never error, got: %v", err)
	}
}

func TestApplyFlags_AllFlags(t *testing.T) {
	cfg := config.Config{}
	flags := &runFlags{
		neovimVersion: "nightly",
		patterns:      []string{"spec/**/*_spec.lua"},
		coverageDir:   "/tmp/cov",
		formats:       []string{"lcov", "junit"},
		threshold:     75.0,
		cacheDir:      "/tmp/cache",
		verbose:       true,
	}
	applyFlags(&cfg, flags)

	if cfg.NeovimVersion != "nightly" {
		t.Errorf("NeovimVersion = %q, want %q", cfg.NeovimVersion, "nightly")
	}
	if len(cfg.TestPatterns) != 1 || cfg.TestPatterns[0] != "spec/**/*_spec.lua" {
		t.Errorf("TestPatterns = %v", cfg.TestPatterns)
	}
	if cfg.CoverageDir != "/tmp/cov" {
		t.Errorf("CoverageDir = %q, want /tmp/cov", cfg.CoverageDir)
	}
	if len(cfg.Formats) != 2 {
		t.Errorf("Formats = %v", cfg.Formats)
	}
	if cfg.Threshold != 75.0 {
		t.Errorf("Threshold = %v, want 75.0", cfg.Threshold)
	}
	if cfg.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q, want /tmp/cache", cfg.CacheDir)
	}
	if !cfg.Verbose {
		t.Errorf("Verbose = false, want true")
	}
}

func TestApplyFlags_NoOverride(t *testing.T) {
	cfg := config.Config{
		NeovimVersion: "stable",
		CoverageDir:   "coverage",
	}
	flags := &runFlags{} // all zero values — nothing should be overridden
	applyFlags(&cfg, flags)

	if cfg.NeovimVersion != "stable" {
		t.Errorf("NeovimVersion should not be overridden, got %q", cfg.NeovimVersion)
	}
	if cfg.CoverageDir != "coverage" {
		t.Errorf("CoverageDir should not be overridden, got %q", cfg.CoverageDir)
	}
}

func TestReporterFor_Console(t *testing.T) {
	r, f, err := reporterFor("console", config.Config{}, false)
	if err != nil {
		t.Fatalf("reporterFor(console) error: %v", err)
	}
	if r == nil {
		t.Error("reporterFor(console) returned nil reporter")
	}
	if f != os.Stdout {
		t.Error("reporterFor(console) should return os.Stdout")
	}
}

func TestReporterFor_FileFormats(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{CoverageDir: dir}

	formats := []string{"lcov", "cobertura", "coveralls", "junit"}
	for _, format := range formats {
		format := format
		t.Run(format, func(t *testing.T) {
			r, f, err := reporterFor(format, cfg, false)
			if err != nil {
				t.Fatalf("reporterFor(%q) error: %v", format, err)
			}
			if f != nil {
				t.Cleanup(func() { f.Close() })
			}
			if r == nil {
				t.Errorf("reporterFor(%q) returned nil reporter", format)
			}
			if f == nil {
				t.Errorf("reporterFor(%q) returned nil file", format)
			}
		})
	}
}

func TestReporterFor_Unknown(t *testing.T) {
	_, _, err := reporterFor("unknown-format", config.Config{}, false)
	if err == nil {
		t.Error("reporterFor(unknown) expected error")
	}
}

func TestEmitReports_Console(t *testing.T) {
	cfg := config.Config{Formats: []string{"console"}}
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{{Name: "test", Status: domain.StatusPass}},
	}
	cov := &domain.CoverageData{}

	if err := emitReports(context.Background(), cfg, suite, cov); err != nil {
		t.Errorf("emitReports() console error: %v", err)
	}
}

func TestEmitReports_FileFormat(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Formats:     []string{"lcov"},
		CoverageDir: dir,
	}
	suite := &domain.SuiteResult{}
	cov := &domain.CoverageData{}

	if err := emitReports(context.Background(), cfg, suite, cov); err != nil {
		t.Errorf("emitReports() lcov error: %v", err)
	}
}

func TestEmitReports_UnknownFormat(t *testing.T) {
	cfg := config.Config{Formats: []string{"unknown"}}
	suite := &domain.SuiteResult{}
	cov := &domain.CoverageData{}

	if err := emitReports(context.Background(), cfg, suite, cov); err == nil {
		t.Error("emitReports() expected error for unknown format")
	}
}

func TestReporterFor_FileFormats_CreateError(t *testing.T) {
	// A nonexistent directory causes os.Create to fail for all file-based formats.
	cfg := config.Config{CoverageDir: "/nonexistent/path/that/does/not/exist"}

	formats := []string{"lcov", "cobertura", "coveralls", "junit"}
	for _, format := range formats {
		format := format
		t.Run(format, func(t *testing.T) {
			_, _, err := reporterFor(format, cfg, false)
			if err == nil {
				t.Errorf("reporterFor(%q) expected error for nonexistent dir, got nil", format)
			}
		})
	}
}

func TestNewRunCmd(t *testing.T) {
	cmd := NewRunCmd()
	if cmd == nil {
		t.Fatal("NewRunCmd() returned nil")
	}
	if cmd.Use != "run" {
		t.Errorf("cmd.Use = %q, want %q", cmd.Use, "run")
	}
}

// TestProvisionNeovim_Success exercises provisionNeovim with an injected provider.
func TestProvisionNeovim_Success(t *testing.T) {
	cfg := config.Config{Verbose: false}
	version, _ := domain.ParseVersion("stable")
	platform, _ := domain.CurrentPlatform()

	provider := &fakeNeovimProvider{path: "/fake/nvim"}
	got, err := provisionNeovim(context.Background(), cfg, version, platform, provider)
	if err != nil {
		t.Fatalf("provisionNeovim() error: %v", err)
	}
	if got != "/fake/nvim" {
		t.Errorf("got %q, want %q", got, "/fake/nvim")
	}
}

// TestProvisionNeovim_Verbose exercises the verbose print branch in provisionNeovim.
func TestProvisionNeovim_Verbose(t *testing.T) {
	cfg := config.Config{Verbose: true}
	version, _ := domain.ParseVersion("stable")
	platform, _ := domain.CurrentPlatform()

	provider := &fakeNeovimProvider{path: "/fake/nvim"}
	_, err := provisionNeovim(context.Background(), cfg, version, platform, provider)
	if err != nil {
		t.Fatalf("provisionNeovim() verbose error: %v", err)
	}
}

// TestProvisionNeovim_Error exercises the error path in provisionNeovim.
func TestProvisionNeovim_Error(t *testing.T) {
	cfg := config.Config{}
	version, _ := domain.ParseVersion("stable")
	platform, _ := domain.CurrentPlatform()

	provider := &fakeNeovimProvider{err: fmt.Errorf("download failed")}
	_, err := provisionNeovim(context.Background(), cfg, version, platform, provider)
	if err == nil {
		t.Fatal("provisionNeovim() expected error, got nil")
	}
}

// TestExecuteTests_NoFiles exercises the "no test files found" branch.
func TestExecuteTests_NoFiles(t *testing.T) {
	cfg := config.Config{TestPatterns: []string{"test/**/*_spec.lua"}}
	tr := &fakeTestRunner{files: []string{}}

	suite, cov, err := executeTests(context.Background(), cfg, tr)
	if err != nil {
		t.Fatalf("executeTests() error: %v", err)
	}
	if suite != nil || cov != nil {
		t.Error("expected nil suite and cov when no files found")
	}
}

// TestExecuteTests_DiscoverError exercises the Discover error path.
func TestExecuteTests_DiscoverError(t *testing.T) {
	cfg := config.Config{}
	tr := &fakeTestRunner{discoverErr: fmt.Errorf("glob failed")}

	_, _, err := executeTests(context.Background(), cfg, tr)
	if err == nil {
		t.Fatal("executeTests() expected error, got nil")
	}
}

// TestExecuteTests_Success exercises the happy path with fake test results and
// a real temp directory for CoverageDir.
func TestExecuteTests_Success(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{CoverageDir: dir}
	tr := &fakeTestRunner{
		files: []string{"spec/a_spec.lua"},
		suite: &domain.SuiteResult{Tests: []domain.TestResult{{Name: "a", Status: domain.StatusPass}}},
		cov:   &domain.CoverageData{},
	}

	suite, cov, err := executeTests(context.Background(), cfg, tr)
	if err != nil {
		t.Fatalf("executeTests() error: %v", err)
	}
	if suite == nil {
		t.Fatal("expected non-nil suite")
	}
	if cov == nil {
		t.Fatal("expected non-nil cov")
	}
}

// TestExecuteTests_RunError exercises the error path when testRunner.Run fails.
func TestExecuteTests_RunError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{CoverageDir: dir}
	tr := &fakeTestRunner{
		files:  []string{"spec/a_spec.lua"},
		runErr: fmt.Errorf("nvim crash"),
	}
	_, _, err := executeTests(context.Background(), cfg, tr)
	if err == nil {
		t.Fatal("executeTests() expected error from Run, got nil")
	}
}

// TestRunTests_NeovimProviderError exercises the runTests error path when
// neovim provisioning fails.
func TestRunTests_NeovimProviderError(t *testing.T) {
	flags := &runFlags{} // empty → loads config defaults
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{err: fmt.Errorf("network timeout")},
	}
	err := runTests(context.Background(), flags, deps)
	if err == nil {
		t.Fatal("runTests() expected error, got nil")
	}
}

// TestRunTests_ConfigLoadError exercises the config.Load error branch.
func TestRunTests_ConfigLoadError(t *testing.T) {
	// Pass a directory as the config path — os.ReadFile returns an error that is
	// not os.IsNotExist, so loadTOML propagates it.
	flags := &runFlags{configPath: t.TempDir()}
	err := runTests(context.Background(), flags, runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
	})
	if err == nil {
		t.Fatal("runTests() expected config load error, got nil")
	}
}

// TestRunTests_ParseVersionError exercises the domain.ParseVersion error branch.
func TestRunTests_ParseVersionError(t *testing.T) {
	flags := &runFlags{neovimVersion: "not-a-valid-version!!!"}
	err := runTests(context.Background(), flags, runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
	})
	if err == nil {
		t.Fatal("runTests() expected version parse error, got nil")
	}
}

// TestRunTests_ExecuteTestsError exercises the executeTests error branch.
func TestRunTests_ExecuteTestsError(t *testing.T) {
	flags := &runFlags{}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		testRunner:     &fakeTestRunner{discoverErr: fmt.Errorf("glob failed")},
	}
	err := runTests(context.Background(), flags, deps)
	if err == nil {
		t.Fatal("runTests() expected executeTests error, got nil")
	}
}

// TestRunTests_NoFiles exercises the suite==nil early-return path.
func TestRunTests_NoFiles(t *testing.T) {
	flags := &runFlags{}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		testRunner:     &fakeTestRunner{files: []string{}},
	}
	if err := runTests(context.Background(), flags, deps); err != nil {
		t.Fatalf("runTests() expected nil when no files found, got: %v", err)
	}
}

// TestRunTests_EmitReportsError exercises the emitReports error branch by using
// an unknown format so reporterFor fails inside emitReports.
func TestRunTests_EmitReportsError(t *testing.T) {
	dir := t.TempDir()
	flags := &runFlags{
		formats:     []string{"unknown-format"},
		coverageDir: dir,
	}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		testRunner: &fakeTestRunner{
			files: []string{"spec/a_spec.lua"},
			suite: &domain.SuiteResult{Tests: []domain.TestResult{{Name: "a", Status: domain.StatusPass}}},
			cov:   &domain.CoverageData{},
		},
	}
	err := runTests(context.Background(), flags, deps)
	if err == nil {
		t.Fatal("runTests() expected emitReports error, got nil")
	}
}

// TestRunTests_ThresholdFailed exercises the checkThreshold error branch.
func TestRunTests_ThresholdFailed(t *testing.T) {
	dir := t.TempDir()
	flags := &runFlags{
		threshold:   80.0,
		formats:     []string{"console"},
		coverageDir: dir,
	}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		testRunner: &fakeTestRunner{
			files: []string{"spec/a_spec.lua"},
			suite: &domain.SuiteResult{Tests: []domain.TestResult{{Name: "a", Status: domain.StatusPass}}},
			cov:   &domain.CoverageData{}, // 0% coverage
		},
	}
	err := runTests(context.Background(), flags, deps)
	if err == nil {
		t.Fatal("runTests() expected threshold error, got nil")
	}
}

// TestRunTests_SuiteFailed exercises the suite.Passed() == false error branch.
func TestRunTests_SuiteFailed(t *testing.T) {
	dir := t.TempDir()
	flags := &runFlags{
		formats:     []string{"console"},
		coverageDir: dir,
	}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		testRunner: &fakeTestRunner{
			files: []string{"spec/a_spec.lua"},
			suite: &domain.SuiteResult{Tests: []domain.TestResult{{Name: "a", Status: domain.StatusFail}}},
			cov:   &domain.CoverageData{},
		},
	}
	err := runTests(context.Background(), flags, deps)
	if err == nil {
		t.Fatal("runTests() expected suite-failed error, got nil")
	}
}

// TestRunTests_Success exercises the full happy path returning nil.
func TestRunTests_Success(t *testing.T) {
	dir := t.TempDir()
	flags := &runFlags{
		formats:     []string{"console"},
		coverageDir: dir,
	}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		testRunner: &fakeTestRunner{
			files: []string{"spec/a_spec.lua"},
			suite: &domain.SuiteResult{Tests: []domain.TestResult{{Name: "a", Status: domain.StatusPass}}},
			cov:   &domain.CoverageData{},
		},
	}
	if err := runTests(context.Background(), flags, deps); err != nil {
		t.Fatalf("runTests() expected success, got: %v", err)
	}
}

// TestExecuteTests_Verbose exercises the verbose-print branch when files are found.
func TestExecuteTests_Verbose(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{CoverageDir: dir, Verbose: true}
	tr := &fakeTestRunner{
		files: []string{"spec/a_spec.lua"},
		suite: &domain.SuiteResult{},
		cov:   &domain.CoverageData{},
	}
	if _, _, err := executeTests(context.Background(), cfg, tr); err != nil {
		t.Fatalf("executeTests() verbose error: %v", err)
	}
}

// TestExecuteTests_MkdirAllError exercises the coverage-dir creation error branch.
func TestExecuteTests_MkdirAllError(t *testing.T) {
	// Create a file; using it as a path component causes MkdirAll to fail.
	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// CoverageDir whose parent is a file — os.MkdirAll cannot traverse it.
	cfg := config.Config{CoverageDir: filepath.Join(blockingFile, "subdir")}
	tr := &fakeTestRunner{
		files: []string{"spec/a_spec.lua"},
		suite: &domain.SuiteResult{},
		cov:   &domain.CoverageData{},
	}
	_, _, err := executeTests(context.Background(), cfg, tr)
	if err == nil {
		t.Fatal("executeTests() expected MkdirAll error, got nil")
	}
}
