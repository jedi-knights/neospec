// Package config loads and merges neospec configuration from three sources:
// a TOML file, environment variables, and CLI flags. Precedence is:
// CLI flags > environment variables > TOML file > built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the merged, validated configuration for a neospec run.
type Config struct {
	// NeovimVersion is the version tag to download, e.g. "stable", "nightly", or "v0.10.4".
	NeovimVersion string `toml:"neovim_version"`
	// TestPatterns are glob patterns for discovering test files.
	TestPatterns []string `toml:"test_patterns"`
	// CoverageDir is the directory where coverage report files are written.
	CoverageDir string `toml:"coverage_dir"`
	// Formats lists the report formats to emit: lcov, cobertura, coveralls, junit, console.
	Formats []string `toml:"formats"`
	// BadgePatch controls whether neospec patches the README badge.
	BadgePatch bool `toml:"badge_patch"`
	// ReadmePath is the path to the README file for badge patching.
	ReadmePath string `toml:"readme_path"`
	// Threshold is the minimum required coverage percentage; a non-zero value
	// causes neospec to exit non-zero when coverage falls below it.
	Threshold float64 `toml:"threshold"`
	// CacheDir is the directory where downloaded Neovim binaries are stored.
	CacheDir string `toml:"cache_dir"`
	// Verbose enables additional diagnostic output.
	Verbose bool `toml:"verbose"`
}

// defaults returns a Config populated with built-in default values.
func defaults() Config {
	cacheDir := filepath.Join(userCacheDir(), "neospec")
	return Config{
		NeovimVersion: "stable",
		TestPatterns:  []string{"test/**/*_spec.lua"},
		CoverageDir:   "coverage",
		Formats:       []string{"console"},
		BadgePatch:    false,
		ReadmePath:    "README.md",
		Threshold:     0.0,
		CacheDir:      cacheDir,
		Verbose:       false,
	}
}

// Load reads neospec.toml from path (if it exists), then overlays environment
// variables, and returns the merged Config. CLI flag overrides are applied
// separately via the Apply* methods.
func Load(path string) (Config, error) {
	cfg := defaults()

	if path != "" {
		if err := loadTOML(path, &cfg); err != nil {
			return cfg, err
		}
	}

	applyEnv(&cfg)
	return cfg, nil
}

// loadTOML reads a TOML file into cfg. Missing files are silently ignored;
// malformed files return an error.
func loadTOML(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading config file %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing config file %s: %w", path, err)
	}
	return nil
}

// applyEnv overlays environment variables onto cfg. Only non-empty env vars
// override the current value.
func applyEnv(cfg *Config) {
	if v := os.Getenv("NEOSPEC_NEOVIM_VERSION"); v != "" {
		cfg.NeovimVersion = v
	}
	if v := os.Getenv("NEOSPEC_TEST_PATTERNS"); v != "" {
		cfg.TestPatterns = strings.Split(v, ",")
	}
	if v := os.Getenv("NEOSPEC_COVERAGE_DIR"); v != "" {
		cfg.CoverageDir = v
	}
	if v := os.Getenv("NEOSPEC_FORMATS"); v != "" {
		cfg.Formats = strings.Split(v, ",")
	}
	if v := os.Getenv("NEOSPEC_BADGE_PATCH"); v == "true" || v == "1" {
		cfg.BadgePatch = true
	}
	if v := os.Getenv("NEOSPEC_README_PATH"); v != "" {
		cfg.ReadmePath = v
	}
	if v := os.Getenv("NEOSPEC_CACHE_DIR"); v != "" {
		cfg.CacheDir = v
	}
	if v := os.Getenv("NEOSPEC_VERBOSE"); v == "true" || v == "1" {
		cfg.Verbose = true
	}
}

// userCacheDir returns os.UserCacheDir() with a fallback to ~/.cache so we
// never produce an empty path even on minimal systems.
func userCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
}
