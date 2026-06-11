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
	"time"

	"github.com/jedi-knights/neospec/internal/ports"
)

// removeAllRetryBackoffs is the sleep schedule between RemoveAll attempts in
// Close(). A grandchild process spawned by the test (e.g. a git clone started by
// lazy.nvim) can keep writing into the sandbox tree for a brief window after
// Neovim exits, making os.RemoveAll race the writer and fail with ENOTEMPTY
// ("directory not empty"). The runner kills the Neovim process group before
// Close (see runner.killProcessGroup), so by the time we get here the writer is
// almost always already gone — these bounded retries only cover the gap between
// SIGKILL and the kernel reaping in-flight writes. Total budget < 1s.
var removeAllRetryBackoffs = []time.Duration{
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
}

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
	// backoffs is the retry schedule used by Close; sleep is the wait function
	// between attempts. Both are injectable so tests can exercise the retry loop
	// without real delays.
	backoffs []time.Duration
	sleep    func(time.Duration)
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

	return &xdgSandbox{root: root, fs: f.fs, backoffs: removeAllRetryBackoffs, sleep: time.Sleep}, nil
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

// Close removes all temporary directories created for this sandbox. It retries
// RemoveAll with a bounded backoff because a grandchild process may still be
// writing into the tree just after Neovim exits, which would otherwise fail the
// first attempt with "directory not empty" (see removeAllRetryBackoffs). The
// retry is bounded: a genuinely unremovable sandbox still surfaces the error.
func (s *xdgSandbox) Close() error {
	err := s.fs.RemoveAll(s.root)
	for _, backoff := range s.backoffs {
		if err == nil {
			return nil
		}
		s.sleep(backoff)
		err = s.fs.RemoveAll(s.root)
	}
	return err
}
