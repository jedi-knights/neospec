package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestUserCacheDirWith_HappyPath checks the primary branch: UserCacheDir succeeds.
func TestUserCacheDirWith_HappyPath(t *testing.T) {
	d := osDirs{
		userCacheDir: func() (string, error) { return "/cache", nil },
		userHomeDir:  func() (string, error) { return "/home", nil },
		tempDir:      func() string { return "/tmp" },
	}
	if got := userCacheDirWith(d); got != "/cache" {
		t.Errorf("userCacheDirWith() = %q, want %q", got, "/cache")
	}
}

// TestUserCacheDirWith_HomeFallback checks the second branch: UserCacheDir fails
// but UserHomeDir succeeds. This occurs in containers without a cache-dir entry.
func TestUserCacheDirWith_HomeFallback(t *testing.T) {
	d := osDirs{
		userCacheDir: func() (string, error) { return "", fmt.Errorf("no cache dir") },
		userHomeDir:  func() (string, error) { return "/home/user", nil },
		tempDir:      func() string { return "/tmp" },
	}
	want := "/home/user/.cache"
	if got := userCacheDirWith(d); got != want {
		t.Errorf("userCacheDirWith() = %q, want %q", got, want)
	}
}

// TestUserCacheDirWith_TempFallback checks the final branch: both UserCacheDir
// and UserHomeDir fail. This occurs in minimal container environments.
func TestUserCacheDirWith_TempFallback(t *testing.T) {
	d := osDirs{
		userCacheDir: func() (string, error) { return "", fmt.Errorf("no cache dir") },
		userHomeDir:  func() (string, error) { return "", fmt.Errorf("no home dir") },
		tempDir:      func() string { return "/tmp" },
	}
	want := "/tmp/neospec"
	if got := userCacheDirWith(d); got != want {
		t.Errorf("userCacheDirWith() = %q, want %q", got, want)
	}
}

// TestLoad_CacheDirIsAbsolute verifies that the resolved CacheDir is always an
// absolute path, even when the system cache directory is unavailable. A relative
// CacheDir would cause Neovim binaries to be cached in the process working
// directory, which changes between runs and breaks the cache.
func TestLoad_CacheDirIsAbsolute(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !filepath.IsAbs(cfg.CacheDir) {
		t.Errorf("CacheDir = %q, want absolute path", cfg.CacheDir)
	}
}

// TestLoad_Defaults checks that Load("") returns all expected default values.
func TestLoad_Defaults(t *testing.T) {
	// Clear env vars that would override defaults.
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
		"NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NeovimVersion != "stable" {
		t.Errorf("NeovimVersion = %q, want %q", cfg.NeovimVersion, "stable")
	}
	if len(cfg.TestPatterns) != 1 || cfg.TestPatterns[0] != "test/**/*_spec.lua" {
		t.Errorf("TestPatterns = %v, want [test/**/*_spec.lua]", cfg.TestPatterns)
	}
	if cfg.CoverageDir != "coverage" {
		t.Errorf("CoverageDir = %q, want %q", cfg.CoverageDir, "coverage")
	}
	if len(cfg.Formats) != 1 || cfg.Formats[0] != "console" {
		t.Errorf("Formats = %v, want [console]", cfg.Formats)
	}
	if cfg.Threshold != 0 {
		t.Errorf("Threshold = %v, want 0", cfg.Threshold)
	}
	if cfg.Verbose {
		t.Errorf("Verbose = true, want false")
	}
	if cfg.InitFile != "" {
		t.Errorf("InitFile = %q, want empty string", cfg.InitFile)
	}
}

// TestLoad_TOMLFile checks that a TOML config file is read and merged correctly.
func TestLoad_TOMLFile(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "neospec.toml")
	content := `
neovim_version = "nightly"
test_patterns = ["spec/**/*_spec.lua"]
coverage_dir = "cov"
formats = ["lcov", "junit"]
threshold = 75.0
verbose = true
init_file = "tests/minimal_init.lua"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing TOML: %v", err)
	}

	// Clear env overrides so TOML values are not masked by the test environment.
	// NEOSPEC_CACHE_DIR must be cleared too — if it is set in the outer environment,
	// the TOML's (absent) cache_dir would be silently overridden by the env var.
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
		"NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NeovimVersion != "nightly" {
		t.Errorf("NeovimVersion = %q, want %q", cfg.NeovimVersion, "nightly")
	}
	if len(cfg.TestPatterns) != 1 || cfg.TestPatterns[0] != "spec/**/*_spec.lua" {
		t.Errorf("TestPatterns = %v", cfg.TestPatterns)
	}
	if cfg.CoverageDir != "cov" {
		t.Errorf("CoverageDir = %q, want %q", cfg.CoverageDir, "cov")
	}
	if len(cfg.Formats) != 2 {
		t.Errorf("Formats = %v, want [lcov junit]", cfg.Formats)
	}
	if cfg.Threshold != 75.0 {
		t.Errorf("Threshold = %v, want 75.0", cfg.Threshold)
	}
	if !cfg.Verbose {
		t.Errorf("Verbose = false, want true")
	}
	if cfg.InitFile != "tests/minimal_init.lua" {
		t.Errorf("InitFile = %q, want %q", cfg.InitFile, "tests/minimal_init.lua")
	}
}

// TestLoad_TOMLMissing checks that a missing TOML file is silently ignored.
func TestLoad_TOMLMissing(t *testing.T) {
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
		"NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}
	cfg, err := Load("/nonexistent/path/neospec.toml")
	if err != nil {
		t.Errorf("Load() on missing file should not error, got: %v", err)
	}
	if cfg.NeovimVersion != "stable" {
		t.Errorf("NeovimVersion = %q, want default %q", cfg.NeovimVersion, "stable")
	}
}

// TestLoad_TOMLMalformed checks that a malformed TOML file returns an error.
func TestLoad_TOMLMalformed(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(tomlPath, []byte("not valid toml !! {{{"), 0o644); err != nil {
		t.Fatalf("writing TOML: %v", err)
	}
	_, err := Load(tomlPath)
	if err == nil {
		t.Error("Load() on malformed TOML should return error")
	}
}

// TestLoad_EnvVarOverrides checks that environment variables override the defaults.
func TestLoad_EnvVarOverrides(t *testing.T) {
	t.Setenv("NEOSPEC_NEOVIM_VERSION", "v0.10.0")
	t.Setenv("NEOSPEC_TEST_PATTERNS", "a/**,b/**")
	t.Setenv("NEOSPEC_COVERAGE_DIR", "/tmp/cov")
	t.Setenv("NEOSPEC_FORMATS", "cobertura,coveralls")
	t.Setenv("NEOSPEC_CACHE_DIR", "/tmp/cache")
	t.Setenv("NEOSPEC_VERBOSE", "true")
	t.Setenv("NEOSPEC_INIT_FILE", "/env/init.lua")
	// NEOSPEC_THRESHOLD is not under test here; clear it with the others so
	// an ambient env var does not bleed into an unrelated assertion.
	t.Setenv("NEOSPEC_THRESHOLD", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NeovimVersion != "v0.10.0" {
		t.Errorf("NeovimVersion = %q, want %q", cfg.NeovimVersion, "v0.10.0")
	}
	if len(cfg.TestPatterns) != 2 || cfg.TestPatterns[0] != "a/**" || cfg.TestPatterns[1] != "b/**" {
		t.Errorf("TestPatterns = %v", cfg.TestPatterns)
	}
	if cfg.CoverageDir != "/tmp/cov" {
		t.Errorf("CoverageDir = %q, want /tmp/cov", cfg.CoverageDir)
	}
	if len(cfg.Formats) != 2 || cfg.Formats[0] != "cobertura" {
		t.Errorf("Formats = %v", cfg.Formats)
	}
	if cfg.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q, want /tmp/cache", cfg.CacheDir)
	}
	if !cfg.Verbose {
		t.Errorf("Verbose = false, want true")
	}
	if cfg.InitFile != "/env/init.lua" {
		t.Errorf("InitFile = %q, want %q", cfg.InitFile, "/env/init.lua")
	}
}

// TestLoad_EnvThresholdOverride checks that NEOSPEC_THRESHOLD overrides the
// default threshold value.
func TestLoad_EnvThresholdOverride(t *testing.T) {
	t.Setenv("NEOSPEC_THRESHOLD", "80.5")
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Threshold != 80.5 {
		t.Errorf("Threshold = %v, want 80.5", cfg.Threshold)
	}
}

// TestLoad_TOMLReadError checks that a non-NotExist error from reading the TOML
// file is propagated. Passing a directory path causes os.ReadFile to fail with
// an error that is not os.IsNotExist, triggering the error return in loadTOML.
func TestLoad_TOMLReadError(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir) // directory path, not a file — os.ReadFile returns an error
	if err == nil {
		t.Error("Load() on a directory path should return error")
	}
}

// TestLoad_EnvVerboseFalseOverridesDefault checks that NEOSPEC_VERBOSE=false
// overrides a true value set in a TOML file. This test uses the full Load path
// (TOML → env) to verify that the env override propagates through the call chain.
func TestLoad_EnvVerboseFalseOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "neospec.toml")
	if err := os.WriteFile(tomlPath, []byte("verbose = true\n"), 0o644); err != nil {
		t.Fatalf("writing TOML: %v", err)
	}
	t.Setenv("NEOSPEC_VERBOSE", "false")

	cfg, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Verbose {
		t.Error("Verbose = true, want false when NEOSPEC_VERBOSE=false overrides TOML verbose=true")
	}
}

// TestLoad_VerboseMalformed checks that an unrecognised NEOSPEC_VERBOSE value
// returns an error rather than silently evaluating to false. This is consistent
// with how NEOSPEC_THRESHOLD treats unparseable values. Values like "yes" or
// "TRUE" (case-sensitive mismatch) must not silently misconfigure the run.
func TestLoad_VerboseMalformed(t *testing.T) {
	t.Setenv("NEOSPEC_VERBOSE", "yes")
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_INIT_FILE", "NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}
	_, err := Load("")
	if err == nil {
		t.Error("Load() with NEOSPEC_VERBOSE=yes should return error, got nil")
	}
}

// TestLoad_EnvVarSplitTrimmed checks that individual entries in
// comma-delimited environment variables have whitespace stripped. A value like
// "lcov, junit" (space after comma) should produce ["lcov", "junit"], not
// ["lcov", " junit"] — the latter would not match any known format name.
func TestLoad_EnvVarSplitTrimmed(t *testing.T) {
	t.Setenv("NEOSPEC_FORMATS", " lcov , junit ")
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE", "NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Formats) != 2 || cfg.Formats[0] != "lcov" || cfg.Formats[1] != "junit" {
		t.Errorf("Formats = %v, want [lcov junit] (whitespace should be trimmed from split entries)", cfg.Formats)
	}
}

// TestLoad_EnvVarTrimmed checks that leading and trailing whitespace in
// environment variable values is stripped before parsing. CI pipelines (e.g.
// GitHub Actions env: blocks) occasionally inject trailing newlines or spaces
// from shell interpolation, which would cause strconv.ParseFloat to fail with a
// confusing error message instead of the expected behavior.
func TestLoad_EnvVarTrimmed(t *testing.T) {
	t.Setenv("NEOSPEC_THRESHOLD", " 80.5 ")
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
	} {
		t.Setenv(key, "")
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() with whitespace-padded NEOSPEC_THRESHOLD should not error, got: %v", err)
	}
	if cfg.Threshold != 80.5 {
		t.Errorf("Threshold = %v, want 80.5 (whitespace should be trimmed)", cfg.Threshold)
	}
}

// TestLoad_EnvVarSplitTrimmed_TrailingComma checks that a trailing comma in a
// comma-separated env var does not produce an empty string element. "lcov," should
// yield ["lcov"], not ["lcov", ""] — an empty element would fail format matching
// at runtime with an unhelpful "unknown format" error.
func TestLoad_EnvVarSplitTrimmed_TrailingComma(t *testing.T) {
	t.Setenv("NEOSPEC_FORMATS", "lcov,")
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE", "NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Formats) != 1 || cfg.Formats[0] != "lcov" {
		t.Errorf("Formats = %v, want [lcov] (trailing comma should not produce empty element)", cfg.Formats)
	}
}

// TestLoad_EnvThresholdMalformed checks that a non-numeric NEOSPEC_THRESHOLD
// returns an error rather than silently using the default value.
func TestLoad_EnvThresholdMalformed(t *testing.T) {
	t.Setenv("NEOSPEC_THRESHOLD", "eighty")
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
	} {
		t.Setenv(key, "")
	}
	_, err := Load("")
	if err == nil {
		t.Error("Load() with malformed NEOSPEC_THRESHOLD should return error")
	}
}

// TestLoad_EnvVerboseFlagOne checks that NEOSPEC_VERBOSE=1 enables verbose mode.
func TestLoad_EnvVerboseFlagOne(t *testing.T) {
	t.Setenv("NEOSPEC_VERBOSE", "1")
	// Clear others
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_INIT_FILE", "NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Verbose {
		t.Errorf("Verbose = false, want true for NEOSPEC_VERBOSE=1")
	}
}

// TestLoad_InitFileDefault checks that InitFile defaults to empty string.
func TestLoad_InitFileDefault(t *testing.T) {
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
		"NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.InitFile != "" {
		t.Errorf("InitFile = %q, want empty string by default", cfg.InitFile)
	}
}

// TestLoad_InitFileTOML checks that init_file is read from a TOML config.
func TestLoad_InitFileTOML(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "neospec.toml")
	content := `init_file = "tests/minimal_init.lua"`
	if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing TOML: %v", err)
	}
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_INIT_FILE",
		"NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.InitFile != "tests/minimal_init.lua" {
		t.Errorf("InitFile = %q, want %q", cfg.InitFile, "tests/minimal_init.lua")
	}
}

// TestLoad_InitFileEnvVar checks that NEOSPEC_INIT_FILE overrides the TOML value.
func TestLoad_InitFileEnvVar(t *testing.T) {
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE", "NEOSPEC_THRESHOLD",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("NEOSPEC_INIT_FILE", "/env/init.lua")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.InitFile != "/env/init.lua" {
		t.Errorf("InitFile = %q, want %q", cfg.InitFile, "/env/init.lua")
	}
}
