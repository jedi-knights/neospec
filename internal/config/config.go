// Package config loads and merges neospec configuration from three sources:
// a TOML file, environment variables, and CLI flags. Precedence is:
// CLI flags > environment variables > TOML file > built-in defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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
	// Threshold is the minimum required coverage percentage; a non-zero value
	// causes neospec to exit non-zero when coverage falls below it.
	Threshold float64 `toml:"threshold"`
	// CacheDir is the directory where downloaded Neovim binaries are stored.
	CacheDir string `toml:"cache_dir"`
	// Verbose enables additional diagnostic output.
	Verbose bool `toml:"verbose"`
	// InitFile is an optional path to a Lua file executed before the coverage
	// hook and test harness. Use it to set up runtimepath, mock dependencies, or
	// otherwise configure the Neovim environment before tests run. The file is
	// not instrumented by the coverage hook because it executes before the hook
	// is installed.
	InitFile string `toml:"init_file"`
}

// defaults returns a Config populated with built-in default values.
func defaults() Config {
	cacheDir := filepath.Join(userCacheDirWith(defaultOSDirs), "neospec")
	return Config{
		NeovimVersion: "stable",
		TestPatterns:  []string{"test/**/*_spec.lua"},
		CoverageDir:   "coverage",
		Formats:       []string{"console"},
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

	if err := applyEnv(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// loadTOML reads a TOML file into cfg. Missing files are silently ignored;
// malformed files return an error.
func loadTOML(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
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
// override the current value. It returns an error if a numeric env var is
// present but cannot be parsed, so user configuration mistakes are surfaced
// immediately rather than silently running with a different value.
func applyEnv(cfg *Config) error {
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_NEOVIM_VERSION")); v != "" {
		cfg.NeovimVersion = v
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_TEST_PATTERNS")); v != "" {
		cfg.TestPatterns = splitTrimmed(v, ",")
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_COVERAGE_DIR")); v != "" {
		cfg.CoverageDir = v
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_FORMATS")); v != "" {
		cfg.Formats = splitTrimmed(v, ",")
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_CACHE_DIR")); v != "" {
		cfg.CacheDir = v
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_VERBOSE")); v != "" {
		switch v {
		case "true", "1":
			cfg.Verbose = true
		case "false", "0":
			cfg.Verbose = false
		default:
			return fmt.Errorf("NEOSPEC_VERBOSE %q is not a valid boolean (use true, false, 1, or 0; values are case-sensitive)", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_THRESHOLD")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("NEOSPEC_THRESHOLD %q is not a valid float: %w", v, err)
		}
		cfg.Threshold = f
	}
	if v := strings.TrimSpace(os.Getenv("NEOSPEC_INIT_FILE")); v != "" {
		cfg.InitFile = v
	}
	return nil
}

// splitTrimmed splits s by sep, trims whitespace from each element, and
// removes empty elements. This handles CI-injected env vars like "lcov, junit"
// (space after comma) and trailing commas like "lcov," that would otherwise
// produce empty elements silently failing format matching at runtime.
func splitTrimmed(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// osDirs bundles the OS directory functions used by defaults(). Holding them
// in a struct lets tests inject controlled implementations without changing
// the exported API or relying on OS-level manipulation.
type osDirs struct {
	userCacheDir func() (string, error)
	userHomeDir  func() (string, error)
	tempDir      func() string
}

// defaultOSDirs is the production set of OS directory lookups.
var defaultOSDirs = osDirs{
	userCacheDir: os.UserCacheDir,
	userHomeDir:  os.UserHomeDir,
	tempDir:      os.TempDir,
}

// userCacheDirWith returns an absolute cache directory path using the
// provided OS directory functions, with progressive fallbacks. On minimal
// systems (containers without a passwd entry), both UserCacheDir and
// UserHomeDir may fail; in that case we fall back to the OS temp directory,
// which is guaranteed to be absolute.
func userCacheDirWith(d osDirs) string {
	if dir, err := d.userCacheDir(); err == nil {
		return dir
	}
	if home, err := d.userHomeDir(); err == nil {
		return filepath.Join(home, ".cache")
	}
	return filepath.Join(d.tempDir(), "neospec")
}
