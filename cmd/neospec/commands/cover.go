package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jedi-knights/neospec/internal/adapters/cover"
	"github.com/jedi-knights/neospec/internal/adapters/neovim"
	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/config"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// coverDeps holds injectable dependencies for runCover. Pass coverDeps{}
// in production — nil fields cause real adapters to be constructed. Tests
// inject fakes to avoid touching the network, filesystem, or subprocesses.
type coverDeps struct {
	executor coverExecutor
	stdout   io.Writer
}

// coverExecutor is the abstraction runCover calls into. Matches the shape of
// *cover.Executor so production wires the real adapter and tests wire fakes.
type coverExecutor interface {
	Run(ctx context.Context, opts cover.Opts) (*domain.CoverageData, error)
}

// coverFlags holds values parsed from CLI flags for the cover command.
type coverFlags struct {
	configPath    string
	runner        string
	dir           string
	minimalInit   string
	neovimVersion string
	formats       []string
	coverageDir   string
	threshold     float64
	cacheDir      string
	verbose       bool
}

// NewCoverCmd builds the `neospec cover` command. cover wraps an existing
// Neovim test runner with coverage instrumentation without replacing the
// runner itself — the tj-audience adoption unlock per the neospec adoption
// strategy.
func NewCoverCmd() *cobra.Command {
	flags := &coverFlags{}

	cmd := &cobra.Command{
		Use:   "cover [flags] [-- <external-cmd>...]",
		Short: "Collect coverage while running an existing test framework",
		Long: `cover instruments plenary-busted, mini.test, or a user-supplied external command
with coverage collection, without replacing the runner. It is the companion mode
for teams already invested in a Neovim-native test framework who want coverage
reports (LCOV, Cobertura, Coveralls, console) added to their CI without
rewriting a single test.

For plenary-busted and mini-test modes, cover discovers plenary/mini.test on the
runtimepath your --minimal-init supplies, installs the coverage hook, invokes
the runner programmatically, and emits reports in the requested formats.

For external mode, cover writes the coverage hook to disk, sets NEOSPEC_COVER_HOOK
and NEOSPEC_COVER_OUTPUT env vars, and runs your command — you're responsible
for loading the hook via 'nvim -c "luafile $NEOSPEC_COVER_HOOK"' or equivalent.`,
		Example: `  # Wrap a plenary-busted suite; existing tests unchanged
  neospec cover --runner=plenary-busted --dir=tests/ \
    --minimal-init=tests/minimal_init.vim --format=console --format=lcov

  # Wrap mini.test
  neospec cover --runner=mini-test --dir=tests/ \
    --minimal-init=scripts/minimal_init.lua --format=lcov

  # External mode — user's Makefile calls nvim; power-user opt-in
  neospec cover --runner=external --format=lcov -- make test`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCover(cmd.Context(), flags, args, coverDeps{})
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.configPath, "config", "c", "neospec.toml", "path to config file")
	f.StringVar(&flags.runner, "runner", "", "wrapped runner: plenary-busted, mini-test, or external")
	f.StringVar(&flags.dir, "dir", "", "test directory or glob (required for plenary-busted and mini-test modes)")
	f.StringVar(&flags.minimalInit, "minimal-init", "", "path to a minimal init file that bootstraps plenary or mini.test onto the runtimepath")
	f.StringVar(&flags.neovimVersion, "neovim-version", "", "neovim version to use (e.g. stable, nightly, v0.10.4)")
	f.StringArrayVar(&flags.formats, "format", nil, "output format(s): console, lcov, cobertura, coveralls (repeatable)")
	f.StringVar(&flags.coverageDir, "coverage-dir", "", "directory for coverage report files")
	f.Float64Var(&flags.threshold, "threshold", 0, "minimum coverage percentage (0 = disabled)")
	f.StringVar(&flags.cacheDir, "cache-dir", "", "directory for cached Neovim binaries")
	f.BoolVarP(&flags.verbose, "verbose", "v", false, "verbose output")

	return cmd
}

func runCover(ctx context.Context, flags *coverFlags, args []string, deps coverDeps) error {
	cfg, err := loadCoverConfig(flags)
	if err != nil {
		return err
	}

	opts, err := buildCoverOpts(flags, args, cfg)
	if err != nil {
		return err
	}

	exec := deps.executor
	if exec == nil {
		platform, perr := domain.CurrentPlatform()
		if perr != nil {
			return fmt.Errorf("detecting platform: %w", perr)
		}
		exec = cover.NewExecutor(neovim.NewProvider(cfg.CacheDir), sandbox.NewFactory(), realRunner{}, platform)
	}

	covData, runErr := exec.Run(ctx, opts)
	// covData may be non-nil even when runErr is non-nil (wrapped runner exited
	// non-zero but the reporter fired before the exit). Emit reports first so
	// partial coverage is visible, then surface the runner error at the end.
	out := deps.stdout
	if out == nil {
		out = os.Stdout
	}
	if err := emitCoverReports(ctx, out, cfg, covData); err != nil {
		return err
	}
	if err := checkCoverThreshold(cfg, covData); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

// loadCoverConfig loads the TOML config and overlays only the CLI flags that
// apply to cover. Test discovery, patterns, and initFile settings from the
// config file are ignored — cover has its own semantics.
func loadCoverConfig(flags *coverFlags) (config.Config, error) {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("loading config: %w", err)
	}
	if flags.cacheDir != "" {
		cfg.CacheDir = flags.cacheDir
	}
	if flags.coverageDir != "" {
		cfg.CoverageDir = flags.coverageDir
	}
	if flags.threshold > 0 {
		cfg.Threshold = flags.threshold
	}
	if flags.verbose {
		cfg.Verbose = true
	}
	if flags.neovimVersion != "" {
		cfg.NeovimVersion = flags.neovimVersion
	}
	if len(flags.formats) > 0 {
		cfg.Formats = flags.formats
	}
	if len(cfg.Formats) == 0 {
		cfg.Formats = []string{"console"}
	}
	return cfg, nil
}

// buildCoverOpts translates parsed flags and positional args into the Opts
// struct the cover.Executor consumes. It enforces the per-mode required
// fields upfront so the executor never has to guess.
func buildCoverOpts(flags *coverFlags, args []string, cfg config.Config) (cover.Opts, error) {
	if flags.runner == "" {
		return cover.Opts{}, fmt.Errorf("--runner is required (choose plenary-busted, mini-test, or external)")
	}
	mode := cover.RunnerMode(flags.runner)
	switch mode {
	case cover.RunnerPlenaryBusted, cover.RunnerMiniTest:
		if flags.dir == "" {
			return cover.Opts{}, fmt.Errorf("--dir is required for %s mode", flags.runner)
		}
	case cover.RunnerExternal:
		if len(args) == 0 {
			return cover.Opts{}, fmt.Errorf("external mode requires the wrapped command after -- (e.g. neospec cover --runner=external -- make test)")
		}
	default:
		return cover.Opts{}, fmt.Errorf("unknown --runner %q (choose plenary-busted, mini-test, or external)", flags.runner)
	}

	version, err := domain.ParseVersion(cfg.NeovimVersion)
	if err != nil {
		return cover.Opts{}, fmt.Errorf("parsing neovim version: %w", err)
	}

	return cover.Opts{
		Mode:        mode,
		Version:     version,
		Dir:         flags.dir,
		MinimalInit: flags.minimalInit,
		Command:     args,
		Verbose:     cfg.Verbose,
	}, nil
}

// emitCoverReports writes each configured format. Junit is intentionally not
// supported here — cover mode has no test-suite data to serialize into JUnit.
// A caller who wants test-result output should use neospec run instead.
func emitCoverReports(ctx context.Context, stdout io.Writer, cfg config.Config, cov *domain.CoverageData) error {
	if err := os.MkdirAll(cfg.CoverageDir, 0o755); err != nil {
		return fmt.Errorf("creating coverage dir: %w", err)
	}
	for _, format := range cfg.Formats {
		if err := writeCoverReport(ctx, stdout, cfg, format, cov); err != nil {
			return err
		}
	}
	return nil
}

// writeCoverReport opens, writes, and closes the output for a single report
// format. Coverage-only mode does not populate a SuiteResult, so nil is passed
// to reporters that would otherwise emit test-pass/fail rows.
func writeCoverReport(ctx context.Context, stdout io.Writer, cfg config.Config, format string, cov *domain.CoverageData) (retErr error) {
	r, target, closer, err := coverReporterFor(stdout, format, cfg)
	if err != nil {
		return err
	}
	if closer != nil {
		defer func() {
			if cerr := closer.Close(); cerr != nil && retErr == nil {
				retErr = cerr
			}
		}()
	}
	// Pass an empty SuiteResult (not nil) — the Console reporter unconditionally
	// calls SuiteResult.Counts() and would nil-deref otherwise. Cover mode never
	// populates test results, so zero counts are the semantic truth we want to
	// display alongside the coverage numbers.
	if err := r.Write(ctx, target, &domain.SuiteResult{}, cov); err != nil {
		return fmt.Errorf("writing %s report: %w", format, err)
	}
	return nil
}

// coverReporterFor returns the Reporter, its target writer, and (for
// file-backed formats) an io.Closer the caller must close after writing.
// Console format returns a nil closer since stdout is caller-owned. Junit
// is rejected as unsupported in cover mode.
func coverReporterFor(stdout io.Writer, format string, cfg config.Config) (ports.Reporter, io.Writer, io.Closer, error) {
	switch format {
	case "console":
		return reporter.NewConsole(false), stdout, nil, nil
	case "lcov":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "lcov.info"))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("creating lcov report file: %w", err)
		}
		return reporter.NewLCOV(), f, f, nil
	case "cobertura":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "cobertura.xml"))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("creating cobertura report file: %w", err)
		}
		return reporter.NewCobertura(), f, f, nil
	case "coveralls":
		f, err := os.Create(filepath.Join(cfg.CoverageDir, "coveralls.json"))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("creating coveralls report file: %w", err)
		}
		return reporter.NewCoveralls(), f, f, nil
	case "junit":
		return nil, nil, nil, fmt.Errorf("junit format is not supported in cover mode — cover has no test-suite data to serialize; use neospec run for junit output")
	default:
		return nil, nil, nil, fmt.Errorf("unknown format %q: choose from console, lcov, cobertura, coveralls", format)
	}
}

// checkCoverThreshold mirrors run's threshold check but on cover-mode coverage.
// A nil CoverageData is treated as 0% (matches run's behavior).
func checkCoverThreshold(cfg config.Config, cov *domain.CoverageData) error {
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
