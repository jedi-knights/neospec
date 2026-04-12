// Package sandbox implements ports.SandboxFactory. Each sandbox creates a
// temporary directory tree that mirrors the XDG base directory structure and
// sets the corresponding XDG_* environment variables so Neovim uses them
// exclusively, without touching the user's real config.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jedi-knights/neospec/internal/ports"
)

// fsOps abstracts the OS filesystem operations used by Factory.Create. Holding
// them in an interface lets tests inject fakes that trigger the MkdirTemp and
// MkdirAll error branches without manipulating real filesystem state.
type fsOps interface {
	MkdirTemp(dir, pattern string) (string, error)
	MkdirAll(path string, perm os.FileMode) error
	RemoveAll(path string) error
}

// realFS is the production fsOps implementation that delegates to the os package.
type realFS struct{}

func (realFS) MkdirTemp(dir, pattern string) (string, error) { return os.MkdirTemp(dir, pattern) }
func (realFS) MkdirAll(path string, perm os.FileMode) error  { return os.MkdirAll(path, perm) }
func (realFS) RemoveAll(path string) error                   { return os.RemoveAll(path) }

// xdgSandbox is a single-use XDG environment tied to a temporary directory.
type xdgSandbox struct {
	root string
	fs   fsOps
}

// Factory creates XDG sandboxes.
type Factory struct {
	fs fsOps
}

// NewFactory creates a Factory backed by the real OS filesystem.
func NewFactory() *Factory {
	return &Factory{fs: realFS{}}
}

// Create creates a new sandbox with a unique temporary root directory.
func (f *Factory) Create(_ context.Context) (ports.Sandbox, error) {
	root, err := f.fs.MkdirTemp("", "neospec-sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("creating sandbox temp dir: %w", err)
	}

	// Pre-create all XDG subdirectories so Neovim doesn't encounter missing dirs.
	for _, sub := range []string{"data", "config", "state", "cache", "runtime"} {
		if err := f.fs.MkdirAll(filepath.Join(root, sub), 0o700); err != nil {
			mkdirErr := fmt.Errorf("creating xdg subdir %s: %w", sub, err)
			if cerr := f.fs.RemoveAll(root); cerr != nil {
				return nil, errors.Join(mkdirErr, fmt.Errorf("cleaning up sandbox root: %w", cerr))
			}
			return nil, mkdirErr
		}
	}

	return &xdgSandbox{root: root, fs: f.fs}, nil
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
	return s.fs.RemoveAll(s.root)
}
