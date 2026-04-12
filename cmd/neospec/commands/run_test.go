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

// TestCheckThreshold_NilCov verifies that a nil CoverageData with a non-zero
// threshold returns an error rather than panicking. The ports.TestRunner interface
// does not prohibit a conforming implementation from returning a nil cov.
func TestCheckThreshold_NilCov(t *testing.T) {
	cfg := config.Config{Threshold: 50.0}
	err := checkThreshold(cfg, nil)
	if err == nil {
		t.Error("checkThreshold() with nil cov and non-zero threshold should return error")
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
		initFile:      "tests/init.lua",
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
	if cfg.InitFile != "tests/init.lua" {
		t.Errorf("InitFile = %q, want %q", cfg.InitFile, "tests/init.lua")
	}
}

// TestApplyFlags_ZeroThresholdDoesNotOverride verifies that a zero --threshold
// flag (the flag's default value when not supplied) does not override a non-zero
// threshold already configured via TOML or env. The guard `flags.threshold > 0`
// means zero is treated as "not set", so only positive values take effect.
func TestApplyFlags_ZeroThresholdDoesNotOverride(t *testing.T) {
	cfg := config.Config{Threshold: 70.0}
	flags := &runFlags{threshold: 0}
	applyFlags(&cfg, flags)
	if cfg.Threshold != 70.0 {
		t.Errorf("Threshold = %v, want 70.0 (zero flag should not override)", cfg.Threshold)
	}
}

// TestApplyFlags_NegativeThreshold verifies that a negative --threshold flag
// value does not overwrite the configured threshold. A user who sets
// --threshold=-5 should not accidentally zero-out a TOML-configured threshold;
// only strictly positive values are meaningful minimum-coverage targets.
func TestApplyFlags_NegativeThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 80.0}
	flags := &runFlags{threshold: -5.0}
	applyFlags(&cfg, flags)
	if cfg.Threshold != 80.0 {
		t.Errorf("Threshold = %v, want 80.0 (negative flag should not override)", cfg.Threshold)
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

func TestApplyFlags_InitFile(t *testing.T) {
	cfg := config.Config{}
	flags := &runFlags{initFile: "tests/minimal_init.lua"}
	applyFlags(&cfg, flags)

	if cfg.InitFile != "tests/minimal_init.lua" {
		t.Errorf("InitFile = %q, want %q", cfg.InitFile, "tests/minimal_init.lua")
	}
}

func TestApplyFlags_InitFileNotOverriddenWhenEmpty(t *testing.T) {
	cfg := config.Config{InitFile: "pre-set.lua"}
	flags := &runFlags{} // initFile is empty — should not clear the existing value
	applyFlags(&cfg, flags)

	if cfg.InitFile != "pre-set.lua" {
		t.Errorf("InitFile = %q, want %q (should not be cleared by empty flag)", cfg.InitFile, "pre-set.lua")
	}
}

// TestNewRunCmd_InitFileFlag verifies that the --init-file flag is registered
// on the run command and can be set.
func TestNewRunCmd_InitFileFlag(t *testing.T) {
	cmd := NewRunCmd()
	f := cmd.Flags().Lookup("init-file")
	if f == nil {
		t.Fatal("--init-file flag not registered on run command")
	}
	if f.DefValue != "" {
		t.Errorf("--init-file default = %q, want empty string", f.DefValue)
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
		t.Run(format, func(t *testing.T) {
			r, f, err := reporterFor(format, cfg, false)
			if err != nil {
				t.Fatalf("reporterFor(%q) error: %v", format, err)
			}
			if f != nil {
				t.Cleanup(func() {
					if err := f.Close(); err != nil {
						t.Errorf("closing %s report file: %v", format, err)
					}
				})
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

// TestRunTests_InitFileThreadedToRunner verifies that cfg.InitFile is passed
// through to the runner constructor. Because deps.testRunner bypasses the
// constructor, this test uses deps.runnerFactory to intercept the call.
func TestRunTests_InitFileThreadedToRunner(t *testing.T) {
	var capturedInitFile string
	var factoryCalled bool
	flags := &runFlags{initFile: "tests/init.lua"}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		runnerFactory: func(_ string, _ bool, initFile string, _ []string) ports.TestRunner {
			factoryCalled = true
			capturedInitFile = initFile
			return &fakeTestRunner{files: []string{}}
		},
	}
	// The fake runner returns no files so runTests exits cleanly at the
	// "no test files found" branch. We only care that the factory was called
	// with the right initFile, not the outcome of runTests.
	if err := runTests(context.Background(), flags, deps); err != nil {
		t.Fatalf("runTests() unexpected error: %v", err)
	}
	if !factoryCalled {
		t.Fatal("runnerFactory was never called — runTests did not reach the factory branch")
	}
	if capturedInitFile != "tests/init.lua" {
		t.Errorf("initFile = %q, want %q", capturedInitFile, "tests/init.lua")
	}
}

// TestRunTests_CoverageIncludeThreadedToRunner verifies that cfg.CoverageInclude
// is passed through to the runner constructor so the coverage hook only records
// files matching the specified path patterns.
func TestRunTests_CoverageIncludeThreadedToRunner(t *testing.T) {
	var capturedCoverageInclude []string
	var factoryCalled bool
	flags := &runFlags{coverageInclude: []string{"lua/", "plugin/"}}
	deps := runDeps{
		neovimProvider: &fakeNeovimProvider{path: "/fake/nvim"},
		runnerFactory: func(_ string, _ bool, _ string, coverageInclude []string) ports.TestRunner {
			factoryCalled = true
			capturedCoverageInclude = coverageInclude
			return &fakeTestRunner{files: []string{}}
		},
	}
	if err := runTests(context.Background(), flags, deps); err != nil {
		t.Fatalf("runTests() unexpected error: %v", err)
	}
	if !factoryCalled {
		t.Fatal("runnerFactory was never called")
	}
	want := []string{"lua/", "plugin/"}
	if len(capturedCoverageInclude) != len(want) {
		t.Fatalf("coverageInclude len = %d, want %d; got %v", len(capturedCoverageInclude), len(want), capturedCoverageInclude)
	}
	for i, v := range want {
		if capturedCoverageInclude[i] != v {
			t.Errorf("coverageInclude[%d] = %q, want %q", i, capturedCoverageInclude[i], v)
		}
	}
}

// TestExecuteTests_EmptyCoverageDir verifies that executeTests returns an error
// when CoverageDir is an invalid path. Uses a regular file as a path component
// so os.MkdirAll fails on all platforms — more portable than relying on
// os.MkdirAll("") failing, whose behavior is not guaranteed across platforms.
func TestExecuteTests_EmptyCoverageDir(t *testing.T) {
	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg := config.Config{CoverageDir: filepath.Join(blockingFile, "coverage")}
	tr := &fakeTestRunner{
		files: []string{"spec/a_spec.lua"},
		suite: &domain.SuiteResult{},
		cov:   &domain.CoverageData{},
	}
	_, _, err := executeTests(context.Background(), cfg, tr)
	if err == nil {
		t.Fatal("executeTests() expected error for invalid CoverageDir, got nil")
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
