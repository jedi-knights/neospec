package neovim

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jedi-knights/neospec/internal/domain"
)

// Cache manages the on-disk store of extracted Neovim binaries.
// Layout: <cacheDir>/<version>/<platform>/bin/nvim[.exe]
type Cache struct {
	root string
}

// NewCache creates a Cache rooted at the given directory.
func NewCache(root string) *Cache {
	return &Cache{root: root}
}

// VersionDir returns the cache subdirectory for a version+platform pair.
func (c *Cache) VersionDir(v domain.Version, p domain.Platform) string {
	return filepath.Join(c.root, v.Tag, string(p.OS), string(p.Arch))
}

// Lookup checks whether a cached binary exists and returns its path.
func (c *Cache) Lookup(v domain.Version, p domain.Platform) (string, bool) {
	binPath := filepath.Join(c.VersionDir(v, p), "bin", domain.BinaryName(p))
	if _, err := os.Stat(binPath); err == nil {
		return binPath, true
	}
	return "", false
}

// Extract unpacks the archive at archivePath into the cache directory for the
// given version and platform, and returns the path to the nvim binary.
func (c *Cache) Extract(v domain.Version, p domain.Platform, archivePath string) (string, error) {
	destDir := c.VersionDir(v, p)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	switch p.OS {
	case domain.OSWindows:
		if err := extractZip(archivePath, destDir); err != nil {
			return "", err
		}
	default:
		if err := extractTarGz(archivePath, destDir); err != nil {
			return "", err
		}
	}

	binPath := filepath.Join(destDir, "bin", domain.BinaryName(p))
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("binary not found after extraction at %s: %w", binPath, err)
	}
	return binPath, nil
}

// extractTarGz extracts a .tar.gz archive, stripping the first path component
// so that nvim-linux-x86_64/bin/nvim becomes <destDir>/bin/nvim.
func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		relPath := stripFirstComponent(hdr.Name)
		if relPath == "" {
			continue
		}

		target := filepath.Join(destDir, relPath)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode)
			if err := writeFile(target, tr, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Neovim archives include symlinks; recreate them.
			_ = os.Remove(target) // ignore error — may not exist
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractZip extracts a .zip archive (Windows), stripping the first component.
func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		relPath := stripFirstComponent(f.Name)
		if relPath == "" {
			continue
		}
		target := filepath.Join(destDir, relPath)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		writeErr := writeFile(target, rc, f.Mode())
		rc.Close()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, r)
	return err
}

// stripFirstComponent removes the first path segment from a slash-separated
// path, e.g. "nvim-linux-x86_64/bin/nvim" → "bin/nvim".
func stripFirstComponent(p string) string {
	for i, c := range p {
		if (c == '/' || c == '\\') && i > 0 {
			return filepath.FromSlash(p[i+1:])
		}
	}
	return ""
}
