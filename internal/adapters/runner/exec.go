package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
)

// realCommandRunner is the production CommandRunner that delegates to
// exec.CommandContext. It is the Adapter that wraps the os/exec stdlib API.
type realCommandRunner struct{}

func (realCommandRunner) Run(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
