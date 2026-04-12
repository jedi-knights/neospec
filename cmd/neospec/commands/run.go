// Package commands contains the cobra command implementations for the neospec CLI.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jedi-knights/neospec/internal/adapters/neovim"
	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/adapters/runner"
	"github.com/jedi-knights/neospec/internal/config"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// runDeps holds injectable dependencies for runTests. Pass runDeps{} in
// production — zero-value (nil) fields cause the real adapters to be constructed.
// In tests, set individual fields to inject fakes without touching the network.
//
// testRunner bypasses runner construction entirely — use it when you only need
// to control Discover/Run behaviour. runnerFactory intercepts the constructor
// call itself, letting tests verify that the correct arguments (nvimPath,
// verbose, initFile) are threaded through from config.
type runDeps struct {
	neovimProvider ports.NeovimProvider
	testRunner     ports.TestRunner
	runnerFactory  func(nvimPath string, verbose bool, initFile string) ports.TestRunner
}

// runFlags holds values parsed from CLI flags for the run command.
type runFlags struct {
	configPath    string
	neovimVersion string
	patterns      []string
	coverageDir   string
	formats       []string
	threshold     float64
	cacheDir      string
	verbose       bool
	initFile      string
}

// NewRunCmd builds the `neospec run` (and default) command.
func NewRunCmd() *cobra.Command {
	flags := &runFlags{}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run Lua tests and collect coverage",
		Long:  `Discovers test files, executes them in an isolated Neovim subprocess, and emits reports.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTests(cmd.Context(), flags, runDeps{})
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.configPath, "config", "c", "neospec.toml", "path to config file")
	f.StringVar(&flags.neovimVersion, "neovim-version", "", "neovim version to use (e.g. stable, nightly, v0.10.4)")
	f.StringArrayVar(&flags.patterns, "pattern", nil, "glob pattern(s) for test files (repeatable)")
	f.StringVar(&flags.coverageDir, "coverage-dir", "", "directory for coverage report files")
	f.StringArrayVar(&flags.formats, "format", nil, "output format(s): console, lcov, cobertura, coveralls, junit (repeatable)")
	f.Float64Var(&flags.threshold, "threshold", 0, "minimum coverage percentage (0 = disabled; cannot clear a non-zero value set in the config file)")
	f.StringVar(&flags.cacheDir, "cache-dir", "", "directory for cached Neovim binaries")
	f.BoolVarP(&flags.verbose, "verbose", "v", false, "verbose output")
	f.StringVar(&flags.initFile, "init-file", "", "path to a Lua file executed before the coverage hook (e.g. tests/minimal_init.lua)")

	return cmd
}

func runTests(ctx context.Context, flags *runFlags, deps runDeps) error {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	applyFlags(&cfg, flags)

	version, err := domain.ParseVersion(cfg.NeovimVersion)
	if err != nil {
		return fmt.Errorf("parsing neovim version: %w", err)
	}

	platform, err := domain.CurrentPlatform()
	if err != nil {
		return fmt.Errorf("detecting platform: %w", err)
	}

	nvimProvider := deps.neovimProvider
	if nvimProvider == nil {
		nvimProvider = neovim.NewProvider(cfg.CacheDir)
	}
	nvimPath, err := provisionNeovim(ctx, cfg, version, platform, nvimProvider)
	if err != nil {
		return err
	}

	tr := deps.testRunner
	if tr == nil {
		if deps.runnerFactory != nil {
			tr = deps.runnerFactory(nvimPath, cfg.Verbose, cfg.InitFile)
		} else {
			tr = runner.NewWithDefaultSandbox(nvimPath, cfg.Verbose, cfg.InitFile)
		}
	}
	suite, cov, err := executeTests(ctx, cfg, tr)
	if err != nil {
		return err
	}
	if suite == nil {
		return nil // no test files found
	}

	if err := emitReports(ctx, cfg, suite, cov); err != nil {
		return err
	}

	if err := checkThreshold(cfg, cov); err != nil {
		return err
	}

	if !suite.Passed() {
		return fmt.Errorf("test suite failed")
	}

	return nil
}

// provisionNeovim calls provider.Ensure for the given version and platform and
// returns the path to the nvim binary. The provider is injected so tests can
// supply a fake without touching the filesystem or network.
func provisionNeovim(ctx context.Context, cfg config.Config, version domain.Version, platform domain.Platform, provider ports.NeovimProvider) (string, error) {
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "neospec: platform=%s neovim=%s\n", platform, version)
	}
	nvimPath, err := provider.Ensure(ctx, version, platform)
	if err != nil {
		return "", fmt.Errorf("ensuring neovim binary: %w", err)
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "neospec: nvim binary at %s\n", nvimPath)
	}
	return nvimPath, nil
}

// executeTests discovers and runs test files, then ensures the coverage output
// directory exists. It returns nil suite and coverage (with no error) when no
// test files are found so the caller can exit cleanly. The testRunner is injected
// so tests can supply a fake without spawning Neovim subprocesses.
func executeTests(ctx context.Context, cfg config.Config, testRunner ports.TestRunner) (*domain.SuiteResult, *domain.CoverageData, error) {
	files, err := testRunner.Discover(ctx, cfg.TestPatterns)
	if err != nil {
		return nil, nil, fmt.Errorf("discovering test files: %w", err)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "neospec: no test files found")
		return nil, nil, nil
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "neospec: found %d test file(s)\n", len(files))
	}

	suite, cov, err := testRunner.Run(ctx, files)
	if err != nil {
		return nil, nil, fmt.Errorf("running tests: %w", err)
	}

	if err := os.MkdirAll(cfg.CoverageDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating coverage dir: %w", err)
	}

	return suite, cov, nil
}

// emitReports writes all configured output formats to their respective destinations.
func emitReports(ctx context.Context, cfg config.Config, suite *domain.SuiteResult, cov *domain.CoverageData) error {
	for _, format := range cfg.Formats {
		if err := writeReport(ctx, cfg, format, suite, cov); err != nil {
			return err
		}
	}
	return nil
}

// writeReport opens, writes, and closes the output for a single report format.
// The file handle is deferred so it is closed even if r.Write panics.
func writeReport(ctx context.Context, cfg config.Config, format string, suite *domain.SuiteResult, cov *domain.CoverageData) (retErr error) {
	r, f, err := reporterFor(format, cfg, cfg.Verbose)
	if err != nil {
		return err
	}
	if f != nil && f != os.Stdout {
		defer func() {
			if cerr := f.Close(); cerr != nil && retErr == nil {
				retErr = cerr
			}
		}()
	}
	if err := r.Write(ctx, f, suite, cov); err != nil {
		return fmt.Errorf("writing %s report: %w", format, err)
	}
	return nil
}

// checkThreshold returns an error if the measured coverage falls below the
// configured minimum. A threshold of zero disables the check. A nil cov is
// treated as 0% coverage — a conforming ports.TestRunner may return nil.
func checkThreshold(cfg config.Config, cov *domain.CoverageData) error {
	if cfg.Threshold <= 0 {
		return nil
	}
	pct := 0.0
	if cov != nil {
		pct = cov.Percentage()
	}
	if pct < cfg.Threshold {
		return fmt.Errorf("coverage %.1f%% is below threshold %.1f%%", pct, cfg.Threshold)
	}
	return nil
}

// reporterFor returns the Reporter and output writer for a named format.
// For non-console formats the writer is a file in cfg.CoverageDir.
func reporterFor(format string, cfg config.Config, color bool) (ports.Reporter, *os.File, error) {
	switch format {
	case "console":
		return reporter.NewConsole(color), os.Stdout, nil
	case "lcov":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "lcov.info"))
		if err != nil {
			return nil, nil, fmt.Errorf("creating lcov report file: %w", err)
		}
		return reporter.NewLCOV(), f, nil
	case "cobertura":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "cobertura.xml"))
		if err != nil {
			return nil, nil, fmt.Errorf("creating cobertura report file: %w", err)
		}
		return reporter.NewCobertura(), f, nil
	case "coveralls":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "coveralls.json"))
		if err != nil {
			return nil, nil, fmt.Errorf("creating coveralls report file: %w", err)
		}
		return reporter.NewCoveralls(), f, nil
	case "junit":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "junit.xml"))
		if err != nil {
			return nil, nil, fmt.Errorf("creating junit report file: %w", err)
		}
		return reporter.NewJUnit(), f, nil
	default:
		return nil, nil, fmt.Errorf("unknown format %q: choose from console, lcov, cobertura, coveralls, junit", format)
	}
}

// applyFlags overlays non-zero CLI flag values onto cfg.
func applyFlags(cfg *config.Config, flags *runFlags) {
	if flags.neovimVersion != "" {
		cfg.NeovimVersion = flags.neovimVersion
	}
	if len(flags.patterns) > 0 {
		cfg.TestPatterns = flags.patterns
	}
	if flags.coverageDir != "" {
		cfg.CoverageDir = flags.coverageDir
	}
	if len(flags.formats) > 0 {
		cfg.Formats = flags.formats
	}
	if flags.threshold > 0 {
		cfg.Threshold = flags.threshold
	}
	if flags.cacheDir != "" {
		cfg.CacheDir = flags.cacheDir
	}
	if flags.verbose {
		cfg.Verbose = true
	}
	if flags.initFile != "" {
		cfg.InitFile = flags.initFile
	}
}
