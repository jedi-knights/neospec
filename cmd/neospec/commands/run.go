// Package commands contains the cobra command implementations for the neospec CLI.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jedi-knights/neospec/internal/adapters/badge"
	"github.com/jedi-knights/neospec/internal/adapters/neovim"
	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/adapters/runner"
	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/config"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// runFlags holds values parsed from CLI flags for the run command.
type runFlags struct {
	configPath    string
	neovimVersion string
	patterns      []string
	coverageDir   string
	formats       []string
	badgePatch    bool
	readmePath    string
	threshold     float64
	cacheDir      string
	verbose       bool
}

// NewRunCmd builds the `neospec run` (and default) command.
func NewRunCmd() *cobra.Command {
	flags := &runFlags{}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run Lua tests and collect coverage",
		Long:  `Discovers test files, executes them in an isolated Neovim subprocess, and emits reports.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTests(cmd.Context(), flags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.configPath, "config", "c", "neospec.toml", "path to config file")
	f.StringVar(&flags.neovimVersion, "neovim-version", "", "neovim version to use (e.g. stable, nightly, v0.10.4)")
	f.StringArrayVar(&flags.patterns, "pattern", nil, "glob pattern(s) for test files (repeatable)")
	f.StringVar(&flags.coverageDir, "coverage-dir", "", "directory for coverage report files")
	f.StringArrayVar(&flags.formats, "format", nil, "output format(s): console, lcov, cobertura, coveralls, junit (repeatable)")
	f.BoolVar(&flags.badgePatch, "badge", false, "patch coverage badge in README.md")
	f.StringVar(&flags.readmePath, "readme", "", "path to README for badge patching")
	f.Float64Var(&flags.threshold, "threshold", 0, "minimum coverage percentage (non-zero fails if below)")
	f.StringVar(&flags.cacheDir, "cache-dir", "", "directory for cached Neovim binaries")
	f.BoolVarP(&flags.verbose, "verbose", "v", false, "verbose output")

	return cmd
}

func runTests(ctx context.Context, flags *runFlags) error {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	applyFlags(&cfg, flags)

	version, err := domain.ParseVersion(cfg.NeovimVersion)
	if err != nil {
		return err
	}

	platform, err := domain.CurrentPlatform()
	if err != nil {
		return err
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "neospec: platform=%s neovim=%s\n", platform, version)
	}

	// 1. Ensure Neovim binary is available.
	nvimProvider := neovim.NewProvider(cfg.CacheDir)
	nvimPath, err := nvimProvider.Ensure(ctx, version, platform)
	if err != nil {
		return fmt.Errorf("ensuring neovim binary: %w", err)
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "neospec: nvim binary at %s\n", nvimPath)
	}

	// 2. Discover test files.
	testRunner := runner.New(nvimPath, sandbox.NewFactory(), cfg.Verbose)
	files, err := testRunner.Discover(ctx, cfg.TestPatterns)
	if err != nil {
		return fmt.Errorf("discovering test files: %w", err)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "neospec: no test files found")
		return nil
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "neospec: found %d test file(s)\n", len(files))
	}

	// 3. Run tests.
	suite, cov, err := testRunner.Run(ctx, files)
	if err != nil {
		return fmt.Errorf("running tests: %w", err)
	}

	// 4. Ensure coverage output directory exists.
	if err := os.MkdirAll(cfg.CoverageDir, 0o755); err != nil {
		return fmt.Errorf("creating coverage dir: %w", err)
	}

	// 5. Emit reports.
	for _, format := range cfg.Formats {
		r, f, err := reporterFor(format, cfg, flags.verbose)
		if err != nil {
			return err
		}
		writeErr := r.Write(ctx, f, suite, cov)
		if f != os.Stdout {
			if cerr := f.Close(); cerr != nil && writeErr == nil {
				writeErr = cerr
			}
		}
		if writeErr != nil {
			return fmt.Errorf("writing %s report: %w", format, writeErr)
		}
	}

	// 6. Patch README badge if requested.
	if cfg.BadgePatch {
		patcher := badge.NewPatcher()
		if err := patcher.Patch(ctx, cfg.ReadmePath, cov.Percentage()); err != nil {
			return fmt.Errorf("patching badge: %w", err)
		}
	}

	// 7. Enforce coverage threshold.
	if cfg.Threshold > 0 {
		pct := cov.Percentage()
		if pct < cfg.Threshold {
			return fmt.Errorf("coverage %.1f%% is below threshold %.1f%%", pct, cfg.Threshold)
		}
	}

	// Exit non-zero if any tests failed.
	if !suite.Passed() {
		return fmt.Errorf("test suite failed")
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
			return nil, nil, err
		}
		return reporter.NewLCOV(), f, nil
	case "cobertura":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "cobertura.xml"))
		if err != nil {
			return nil, nil, err
		}
		return reporter.NewCobertura(), f, nil
	case "coveralls":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "coveralls.json"))
		if err != nil {
			return nil, nil, err
		}
		return reporter.NewCoveralls(), f, nil
	case "junit":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "junit.xml"))
		if err != nil {
			return nil, nil, err
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
	if flags.badgePatch {
		cfg.BadgePatch = true
	}
	if flags.readmePath != "" {
		cfg.ReadmePath = flags.readmePath
	}
	if flags.threshold != 0 {
		cfg.Threshold = flags.threshold
	}
	if flags.cacheDir != "" {
		cfg.CacheDir = flags.cacheDir
	}
	if flags.verbose {
		cfg.Verbose = true
	}
}
