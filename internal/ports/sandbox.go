package ports

import "context"

// Sandbox represents an isolated execution environment for a single Neovim
// test invocation. It provides XDG base directory overrides so that the test
// process cannot read or write the user's real Neovim configuration.
type Sandbox interface {
	// Env returns the environment variables that must be set on the Neovim
	// child process to activate this sandbox, in KEY=VALUE form.
	Env() []string
	// Dir returns the root temporary directory of the sandbox, useful for
	// placing generated Lua shim files.
	Dir() string
	// Close removes all temporary directories created for this sandbox.
	// Callers must always call Close, even if the test failed.
	Close() error
}

// SandboxFactory creates new Sandbox instances. One sandbox is created per
// Neovim process invocation.
type SandboxFactory interface {
	Create(ctx context.Context) (Sandbox, error)
}
