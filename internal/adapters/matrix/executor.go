// Package matrix runs a single command against multiple Neovim versions,
// aggregating the outcome of each. It is the adapter behind `neospec exec`.
package matrix

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// Executor runs a fixed command across a series of Neovim versions, one at a
// time. It composes existing adapters — the NeovimProvider resolves each
// version to a binary path, the SandboxFactory produces an isolated XDG
// environment per run, and the CommandRunner spawns the actual process.
type Executor struct {
	neovim   ports.NeovimProvider
	sandbox  ports.SandboxFactory
	cmdRun   ports.CommandRunner
	platform domain.Platform
}

// NewExecutor wires an Executor from its three port dependencies. Callers
// must supply the current platform explicitly so tests can vary it without
// touching runtime.GOOS.
func NewExecutor(
	neovim ports.NeovimProvider,
	sandbox ports.SandboxFactory,
	cmdRun ports.CommandRunner,
	platform domain.Platform,
) *Executor {
	return &Executor{
		neovim:   neovim,
		sandbox:  sandbox,
		cmdRun:   cmdRun,
		platform: platform,
	}
}

// Run executes the given argv against each version in order and returns an
// ExecMatrix aggregating the results. The returned error is non-nil only for
// programmer errors (empty versions, empty command); per-version failures are
// recorded in the ExecMatrix and do not stop iteration.
func (e *Executor) Run(ctx context.Context, argv []string, versions []domain.Version) (domain.ExecMatrix, error) {
	if len(argv) == 0 {
		return domain.ExecMatrix{}, fmt.Errorf("matrix: command is empty")
	}
	if len(versions) == 0 {
		return domain.ExecMatrix{}, fmt.Errorf("matrix: no versions requested")
	}

	m := domain.ExecMatrix{Command: argv, Results: make([]domain.MatrixResult, 0, len(versions))}
	start := time.Now()
	for _, v := range versions {
		m.Results = append(m.Results, e.runOne(ctx, argv, v))
	}
	m.Duration = time.Since(start)
	return m, nil
}

// runOne executes the command against a single Neovim version. Any provisioning
// or process failure is captured in the returned MatrixResult; runOne itself
// never returns an error separate from the result so the caller can continue
// through the remaining versions.
func (e *Executor) runOne(ctx context.Context, argv []string, v domain.Version) domain.MatrixResult {
	start := time.Now()

	nvimPath, err := e.neovim.Ensure(ctx, v, e.platform)
	if err != nil {
		return domain.MatrixResult{Version: v, Err: fmt.Errorf("ensuring neovim %s: %w", v.Tag, err), Duration: time.Since(start)}
	}

	sb, err := e.sandbox.Create(ctx)
	if err != nil {
		return domain.MatrixResult{Version: v, Err: fmt.Errorf("creating sandbox for %s: %w", v.Tag, err), Duration: time.Since(start)}
	}
	defer func() { _ = sb.Close() }()

	env := versionEnv(sb.Env(), nvimPath)
	stdout, stderr, runErr := e.cmdRun.Run(ctx, env, argv[0], argv[1:]...)

	return domain.MatrixResult{
		Version:  v,
		ExitCode: exitCodeFrom(runErr),
		Stdout:   stdout,
		Stderr:   stderr,
		Duration: time.Since(start),
		Err:      wrapUnknownRunErr(runErr),
	}
}

// versionEnv extends the sandbox's environment with a PATH entry that puts
// this version's Neovim binary directory first, so the caller's command can
// invoke `nvim` (or any nvim-spawned subprocess) and get the pinned build.
// The CommandRunner appends this to os.Environ, and Go's exec.Cmd resolves
// duplicate keys with a last-wins rule, so this PATH beats the parent's.
func versionEnv(sandboxEnv []string, nvimPath string) []string {
	nvimDir := filepath.Dir(nvimPath)
	parentPath := os.Getenv("PATH")
	pathEntry := "PATH=" + nvimDir
	if parentPath != "" {
		pathEntry += string(os.PathListSeparator) + parentPath
	}
	env := make([]string, 0, len(sandboxEnv)+1)
	env = append(env, sandboxEnv...)
	env = append(env, pathEntry)
	return env
}

// exitCodeFrom recovers the process exit code from a CommandRunner error.
// Returns 0 for nil errors and for errors that are not process-exit signals
// (those get surfaced separately as MatrixResult.Err).
func exitCodeFrom(err error) int {
	if err == nil {
		return 0
	}
	// exec.ExitError.ExitCode() returns -1 for signals; treat as non-zero.
	type exitCoder interface{ ExitCode() int }
	if ec, ok := err.(exitCoder); ok {
		return ec.ExitCode()
	}
	return 0
}

// wrapUnknownRunErr returns nil for process-exit errors (whose signal is
// carried in ExitCode instead), and the original error otherwise — those
// represent failures like a missing executable or a startup error that never
// produced a process at all.
func wrapUnknownRunErr(err error) error {
	if err == nil {
		return nil
	}
	type exitCoder interface{ ExitCode() int }
	if _, ok := err.(exitCoder); ok {
		return nil
	}
	return err
}
