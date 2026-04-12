package neovim_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/neovim"
	"github.com/jedi-knights/neospec/internal/domain"
)

func TestNewProvider(t *testing.T) {
	p := neovim.NewProvider(t.TempDir())
	if p == nil {
		t.Fatal("NewProvider() returned nil")
	}
}

func TestProvider_Ensure_CacheHit(t *testing.T) {
	cacheDir := t.TempDir()

	// Pre-create the binary so the cache reports a hit.
	v, _ := domain.ParseVersion("stable")
	platform := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}
	binDir := filepath.Join(cacheDir, "stable", "linux", "x86_64", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	binPath := filepath.Join(binDir, "nvim")
	if err := os.WriteFile(binPath, []byte("fake nvim"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	provider := neovim.NewProvider(cacheDir)
	got, err := provider.Ensure(context.Background(), v, platform)
	if err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	if got != binPath {
		t.Errorf("Ensure() = %q, want %q", got, binPath)
	}
}

func TestProvider_Ensure_UnsupportedPlatform(t *testing.T) {
	provider := neovim.NewProvider(t.TempDir())
	v, _ := domain.ParseVersion("stable")
	platform := domain.Platform{OS: domain.OS("unsupported"), Arch: domain.ArchAMD64}

	_, err := provider.Ensure(context.Background(), v, platform)
	if err == nil {
		t.Error("Ensure() expected error for unsupported platform")
	}
}
