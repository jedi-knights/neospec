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
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE",
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
`
	if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing TOML: %v", err)
	}

	// Clear env overrides.
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR", "NEOSPEC_VERBOSE",
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
}

// TestLoad_TOMLMissing checks that a missing TOML file is silently ignored.
func TestLoad_TOMLMissing(t *testing.T) {
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
// overrides a true value already set (e.g. from a TOML file) by applyEnv.
func TestLoad_EnvVerboseFalseOverridesDefault(t *testing.T) {
	// Set verbose to true via an env var first, then override it to false.
	// We test via applyEnv directly since Load always starts from defaults (false).
	// Seed a config with Verbose=true (as if loaded from TOML).
	cfg := Config{Verbose: true}
	t.Setenv("NEOSPEC_VERBOSE", "false")
	applyEnv(&cfg)
	if cfg.Verbose {
		t.Error("Verbose = true, want false when NEOSPEC_VERBOSE=false")
	}
}

// TestLoad_EnvVerboseFlagOne checks that NEOSPEC_VERBOSE=1 enables verbose mode.
func TestLoad_EnvVerboseFlagOne(t *testing.T) {
	t.Setenv("NEOSPEC_VERBOSE", "1")
	// Clear others
	for _, key := range []string{
		"NEOSPEC_NEOVIM_VERSION", "NEOSPEC_TEST_PATTERNS", "NEOSPEC_COVERAGE_DIR",
		"NEOSPEC_FORMATS", "NEOSPEC_CACHE_DIR",
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
