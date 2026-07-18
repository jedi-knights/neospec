package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
)

// RunCommand runs the named executable with the given extra environment
// entries and arguments, returning stdout, stderr, and any non-zero exit
// error. It is a bare-function wrapper around the production CommandRunner
// for callers (like the exec subcommand) that need the same subprocess
// lifecycle — including process-group reaping of Neovim grandchildren —
// without pulling in the rest of the test-runner scaffolding.
func RunCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	return realCommandRunner{}.Run(ctx, env, name, args...)
}

// realCommandRunner is the production CommandRunner that delegates to
// exec.CommandContext. It is the Adapter that wraps the os/exec stdlib API.
type realCommandRunner struct{}

func (realCommandRunner) Run(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the child in its own process group so we can reap its entire subtree.
	// Neovim spawns asynchronous grandchildren (e.g. git clones kicked off by a
	// plugin manager such as lazy.nvim) that outlive the nvim process itself.
	// cmd.Wait() only reaps the direct child, so without this those orphans keep
	// writing into the sandbox dir and race the caller's RemoveAll teardown.
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), err
	}

	waitErr := cmd.Wait()

	// Reap any descendants still running after the direct child exited, so no
	// live writer remains when the sandbox directory is removed. The process
	// group ID stays reserved while any group member is alive, so targeting it
	// here is safe even though the direct child has already been waited on.
	killProcessGroup(cmd)

	return stdout.Bytes(), stderr.Bytes(), waitErr
}
