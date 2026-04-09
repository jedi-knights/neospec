// Package sandbox implements ports.SandboxFactory. Each sandbox creates a
// temporary directory tree that mirrors the XDG base directory structure and
// sets the corresponding XDG_* environment variables so Neovim uses them
// exclusively, without touching the user's real config.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jedi-knights/neospec/internal/ports"
)

// xdgSandbox is a single-use XDG environment tied to a temporary directory.
type xdgSandbox struct {
	root string
}

// Factory creates XDG sandboxes.
type Factory struct{}

// NewFactory creates a Factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Create creates a new sandbox with a unique temporary root directory.
func (f *Factory) Create(_ context.Context) (ports.Sandbox, error) {
	root, err := os.MkdirTemp("", "neospec-sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("creating sandbox temp dir: %w", err)
	}

	// Pre-create all XDG subdirectories so Neovim doesn't encounter missing dirs.
	for _, sub := range []string{"data", "config", "state", "cache", "runtime"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o700); err != nil {
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("creating xdg subdir %s: %w", sub, err)
		}
	}

	return &xdgSandbox{root: root}, nil
}

// Env returns the environment variables that activate this sandbox.
func (s *xdgSandbox) Env() []string {
	return []string{
		"XDG_DATA_HOME=" + filepath.Join(s.root, "data"),
		"XDG_CONFIG_HOME=" + filepath.Join(s.root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(s.root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(s.root, "cache"),
		"XDG_RUNTIME_DIR=" + filepath.Join(s.root, "runtime"),
		// Prevent Neovim from reading system init files.
		"NVIM_APPNAME=neospec-isolated",
		// HOME override ensures no ~/.config/nvim fallback.
		"HOME=" + s.root,
	}
}

// Dir returns the root temporary directory of the sandbox.
func (s *xdgSandbox) Dir() string {
	return s.root
}

// Close removes all temporary directories created for this sandbox.
func (s *xdgSandbox) Close() error {
	return os.RemoveAll(s.root)
}
