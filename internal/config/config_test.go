package config

import (
	"path/filepath"
	"testing"
)

// TestLoad_CacheDirIsAbsolute verifies that the resolved CacheDir is always an
// absolute path, even when the system cache directory is unavailable. A relative
// CacheDir would cause Neovim binaries to be cached in the process working
// directory, which changes between runs and breaks the cache.
func TestLoad_CacheDirIsAbsolute(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !filepath.IsAbs(cfg.CacheDir) {
		t.Errorf("CacheDir = %q, want absolute path", cfg.CacheDir)
	}
}
