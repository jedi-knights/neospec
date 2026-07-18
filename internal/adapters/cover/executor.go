package cover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jedi-knights/neospec/internal/adapters/runner"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// Executor runs an existing test framework (plenary-busted, mini.test, or a
// user-supplied external command) under a coverage-collecting Neovim, then
// returns the aggregated CoverageData. Test-result parsing is deliberately
// out of scope — cover mode wraps runners whose output shape neospec does
// not own, so only the coverage side is reported back to the caller.
type Executor struct {
	neovim   ports.NeovimProvider
	sandbox  ports.SandboxFactory
	cmdRun   ports.CommandRunner
	platform domain.Platform
}

// NewExecutor wires an Executor from its port dependencies.
func NewExecutor(
	neovim ports.NeovimProvider,
	sandbox ports.SandboxFactory,
	cmdRun ports.CommandRunner,
	platform domain.Platform,
) *Executor {
	return &Executor{neovim: neovim, sandbox: sandbox, cmdRun: cmdRun, platform: platform}
}

// Opts is the input to Executor.Run. It carries both the runner-mode
// selection and the Neovim provisioning parameters the executor needs.
type Opts struct {
	// Mode selects the wrapped-runner shape.
	Mode RunnerMode
	// Version is the Neovim release to instrument. If Version.Tag is empty,
	// domain.ParseVersion("stable") is used.
	Version domain.Version
	// Dir is the test directory or file the wrapped runner scans. Required
	// for plenary-busted and mini-test modes.
	Dir string
	// MinimalInit is the path to the user's minimal_init file that bootstraps
	// the wrapped runner (typically plenary or mini.nvim). Empty is allowed;
	// nvim will start with no init.
	MinimalInit string
	// Command is the shell-shape argv for external mode. The first element
	// is the executable; the rest are arguments. Unused for named modes.
	Command []string
	// Verbose enables nvim's -V3 diagnostic output. Off by default.
	Verbose bool
}

// Run instruments the wrapped runner with coverage collection and returns
// the collected CoverageData. The wrapped runner's own test-pass/fail output
// is not parsed by cover mode; if the runner exits non-zero, Run returns an
// error alongside any coverage data that made it to disk.
func (e *Executor) Run(ctx context.Context, opts Opts) (*domain.CoverageData, error) {
	version := opts.Version
	if version.Tag == "" {
		v, err := domain.ParseVersion("stable")
		if err != nil {
			return nil, fmt.Errorf("resolving default version: %w", err)
		}
		version = v
	}

	nvimPath, err := e.neovim.Ensure(ctx, version, e.platform)
	if err != nil {
		return nil, fmt.Errorf("ensuring neovim %s: %w", version.Tag, err)
	}

	sb, err := e.sandbox.Create(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating sandbox: %w", err)
	}
	defer func() { _ = sb.Close() }()

	outputFile := filepath.Join(sb.Dir(), "neospec_cover_output.json")

	switch opts.Mode {
	case RunnerPlenaryBusted, RunnerMiniTest:
		return e.runShim(ctx, sb, nvimPath, opts, outputFile)
	case RunnerExternal:
		return e.runExternal(ctx, sb, opts, outputFile)
	default:
		return nil, fmt.Errorf("cover: unknown runner mode %q", opts.Mode)
	}
}

// runShim handles the plenary-busted and mini-test modes: build a Lua shim
// that installs the coverage hook + reporter, invoke the wrapped runner from
// inside the shim, and read the coverage JSON the reporter writes to disk.
func (e *Executor) runShim(ctx context.Context, sb ports.Sandbox, nvimPath string, opts Opts, outputFile string) (*domain.CoverageData, error) {
	shim, err := BuildShim(ShimOpts{Mode: opts.Mode, Dir: opts.Dir, OutputFile: outputFile})
	if err != nil {
		return nil, err
	}
	shimPath := filepath.Join(sb.Dir(), "neospec_cover_shim.lua")
	if err := os.WriteFile(shimPath, shim, 0o644); err != nil {
		return nil, fmt.Errorf("writing cover shim: %w", err)
	}

	args := []string{"--headless"}
	if opts.MinimalInit != "" {
		args = append(args, "-u", opts.MinimalInit)
	}
	args = append(args, "-l", shimPath)
	if opts.Verbose {
		args = append([]string{"-V3"}, args...)
	}

	_, stderr, runErr := e.cmdRun.Run(ctx, sb.Env(), nvimPath, args...)
	cov, parseErr := readCoverageFile(outputFile)

	// The runner's exit code carries semantic meaning — plenary and mini.test
	// use non-zero exit to signal test failure. We surface a runner error but
	// still return any coverage data that made it to disk before the exit.
	if runErr != nil {
		return cov, fmt.Errorf("wrapped runner exited with error: %w (stderr: %.500s)", runErr, stderr)
	}
	return cov, parseErr
}

// runExternal handles the external mode: set env vars pointing at the hook
// and output file, then invoke the user's literal command. The user is
// responsible for loading the hook (via `nvim -c "luafile $NEOSPEC_COVER_HOOK"`
// or equivalent) and ensuring the reporter fires before nvim exits.
func (e *Executor) runExternal(ctx context.Context, sb ports.Sandbox, opts Opts, outputFile string) (*domain.CoverageData, error) {
	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("cover: external mode requires --cmd")
	}
	hookPath, err := writeExternalHook(sb.Dir())
	if err != nil {
		return nil, err
	}

	env := append([]string{}, sb.Env()...)
	env = append(env,
		"NEOSPEC_COVER_HOOK="+hookPath,
		"NEOSPEC_COVER_OUTPUT="+outputFile,
	)

	_, stderr, runErr := e.cmdRun.Run(ctx, env, opts.Command[0], opts.Command[1:]...)
	cov, parseErr := readCoverageFile(outputFile)

	if runErr != nil {
		return cov, fmt.Errorf("wrapped command exited with error: %w (stderr: %.500s)", runErr, stderr)
	}
	return cov, parseErr
}

// writeExternalHook writes the coverage hook Lua source to disk in the
// sandbox and returns the path. The user's external command loads it via
// the NEOSPEC_COVER_HOOK env var.
func writeExternalHook(dir string) (string, error) {
	hook, err := runner.CoverageHookSource()
	if err != nil {
		return "", fmt.Errorf("reading coverage hook: %w", err)
	}
	path := filepath.Join(dir, "neospec_cover_hook.lua")
	if err := os.WriteFile(path, hook, 0o644); err != nil {
		return "", fmt.Errorf("writing coverage hook: %w", err)
	}
	return path, nil
}

// readCoverageFile reads the JSON output file the reporter (or the user's
// external command) wrote and unmarshals it into CoverageData via the
// runner's public ParseReporterOutput.
func readCoverageFile(path string) (*domain.CoverageData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("cover output file not written — did the wrapped runner exit before the reporter fired? (%s)", path)
		}
		return nil, fmt.Errorf("reading cover output: %w", err)
	}
	_, cov, err := runner.ParseReporterOutput(data)
	if err != nil {
		return nil, fmt.Errorf("parsing cover output: %w", err)
	}
	return cov, nil
}
