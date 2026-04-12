package ports

import "context"

// CommandRunner executes a subprocess and captures its output.
// It is the Strategy interface for os/exec, allowing tests to inject a fake
// implementation that returns canned output without spawning a real process.
type CommandRunner interface {
	// Run runs the named executable with the given extra environment entries
	// and arguments, returning its stdout, stderr, and any non-zero exit error.
	Run(ctx context.Context, env []string, name string, args ...string) (stdout []byte, stderr []byte, err error)
}
