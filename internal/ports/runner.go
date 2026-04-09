package ports

import (
	"context"

	"github.com/jedi-knights/neospec/internal/domain"
)

// TestRunner discovers and executes Lua test files in a Neovim subprocess.
// It is the central coordinator: it invokes a NeovimProvider to obtain the
// binary, a SandboxFactory per test invocation, and the embedded Lua harness.
type TestRunner interface {
	// Discover finds test files matching the given glob patterns and returns
	// their absolute paths.
	Discover(ctx context.Context, patterns []string) ([]string, error)

	// Run executes the given test files and returns aggregated results and
	// coverage data. Coverage is collected in the same Neovim process that runs
	// the tests (via debug.sethook), so both are returned together.
	Run(ctx context.Context, files []string) (*domain.SuiteResult, *domain.CoverageData, error)
}
