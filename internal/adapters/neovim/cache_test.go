package neovim_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/neovim"
	"github.com/jedi-knights/neospec/internal/domain"
)

// makeTarGz creates a .tar.gz archive at dest containing entries.
// Each key in entries is the archive path (e.g. "nvim-linux-x86_64/bin/nvim"),
// and the value is the file content.
func makeTarGz(t *testing.T, dest string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for name, content := range entries {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Size:     int64(len(content)),
			Mode:     0o755,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar entry: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}

// makeZip creates a .zip archive at dest containing entries.
func makeZip(t *testing.T, dest string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestNewCache(t *testing.T) {
	c := neovim.NewCache("/tmp/cache")
	if c == nil {
		t.Fatal("NewCache() returned nil")
	}
}

func TestCache_VersionDir(t *testing.T) {
	c := neovim.NewCache("/tmp/cache")
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	dir := c.VersionDir(v, p)
	want := filepath.Join("/tmp/cache", "stable", "linux", "x86_64")
	if dir != want {
		t.Errorf("VersionDir() = %q, want %q", dir, want)
	}
}

func TestCache_Lookup_Miss(t *testing.T) {
	c := neovim.NewCache(t.TempDir())
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	path, ok := c.Lookup(v, p)
	if ok {
		t.Errorf("Lookup() on empty cache returned ok=true, path=%q", path)
	}
}

func TestCache_Lookup_Hit(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	// Pre-create the expected binary.
	binDir := filepath.Join(cacheDir, "stable", "linux", "x86_64", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	binPath := filepath.Join(binDir, "nvim")
	if err := os.WriteFile(binPath, []byte("fake nvim"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	path, ok := c.Lookup(v, p)
	if !ok {
		t.Fatal("Lookup() expected hit, got miss")
	}
	if path != binPath {
		t.Errorf("Lookup() path = %q, want %q", path, binPath)
	}
}

func TestCache_Extract_TarGz_Linux(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	archivePath := filepath.Join(t.TempDir(), "nvim-linux-x86_64.tar.gz")
	makeTarGz(t, archivePath, map[string]string{
		"nvim-linux-x86_64/bin/nvim": "#!/bin/sh\necho nvim",
	})

	binPath, err := c.Extract(v, p, archivePath)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}

func TestCache_Extract_TarGz_WithDirEntry(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSDarwin, Arch: domain.ArchAMD64}

	archivePath := filepath.Join(t.TempDir(), "nvim-macos-x86_64.tar.gz")

	// Include a directory entry and a file entry.
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Directory entry with first component.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "nvim-macos-x86_64/", Mode: 0o755})
	// File entry.
	content := "#!/bin/sh\necho nvim"
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "nvim-macos-x86_64/bin/nvim", Size: int64(len(content)), Mode: 0o755})
	tw.Write([]byte(content))
	tw.Close()
	gw.Close()
	f.Close()

	binPath, err := c.Extract(v, p, archivePath)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}

func TestCache_Extract_TarGz_WithSymlink(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchARM64}

	archivePath := filepath.Join(t.TempDir(), "nvim-linux-arm64.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	content := "nvim binary"
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "nvim-linux-arm64/bin/nvim", Size: int64(len(content)), Mode: 0o755})
	tw.Write([]byte(content))
	// A symlink entry.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "nvim-linux-arm64/bin/nvim-link", Linkname: "nvim"})
	tw.Close()
	gw.Close()
	f.Close()

	binPath, err := c.Extract(v, p, archivePath)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}

func TestCache_Extract_Zip_Windows(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSWindows, Arch: domain.ArchAMD64}

	archivePath := filepath.Join(t.TempDir(), "nvim-win64.zip")
	makeZip(t, archivePath, map[string]string{
		"nvim-win64/bin/nvim.exe": "fake nvim exe",
	})

	binPath, err := c.Extract(v, p, archivePath)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}

func TestCache_Extract_Zip_WithDirEntry(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSWindows, Arch: domain.ArchAMD64}

	archivePath := filepath.Join(t.TempDir(), "nvim-win64.zip")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(f)
	// Directory entry.
	zw.Create("nvim-win64/")
	// File entry.
	w, _ := zw.Create("nvim-win64/bin/nvim.exe")
	w.Write([]byte("fake exe"))
	zw.Close()
	f.Close()

	binPath, err := c.Extract(v, p, archivePath)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}

func TestCache_Extract_TarGz_InvalidArchive(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	badPath := filepath.Join(t.TempDir(), "bad.tar.gz")
	if err := os.WriteFile(badPath, []byte("not a gzip archive"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := c.Extract(v, p, badPath)
	if err == nil {
		t.Error("Extract() expected error for invalid archive")
	}
}

func TestCache_Extract_Zip_InvalidArchive(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSWindows, Arch: domain.ArchAMD64}

	badPath := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(badPath, []byte("not a zip archive"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := c.Extract(v, p, badPath)
	if err == nil {
		t.Error("Extract() expected error for invalid zip")
	}
}

// TestCache_Extract_TarGz_RootLevelEntry verifies that tar entries without a
// path separator (no top-level directory component) are silently skipped.
// This exercises the stripFirstComponent fallthrough case (returns "").
func TestCache_Extract_TarGz_RootLevelEntry(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	archivePath := filepath.Join(t.TempDir(), "nvim-linux-x86_64.tar.gz")

	// Create an archive that includes a root-level entry (no parent directory),
	// plus the actual binary at the expected path.
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Root-level entry with no parent separator — stripFirstComponent returns "".
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "README", Size: 6, Mode: 0o644})
	tw.Write([]byte("readme"))

	// Valid binary entry.
	content := "nvim binary"
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "nvim-linux-x86_64/bin/nvim", Size: int64(len(content)), Mode: 0o755})
	tw.Write([]byte(content))

	tw.Close()
	gw.Close()
	f.Close()

	binPath, err := c.Extract(v, p, archivePath)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}

func TestCache_Extract_TarGz_MissingBinary(t *testing.T) {
	cacheDir := t.TempDir()
	c := neovim.NewCache(cacheDir)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	// Archive without bin/nvim — Extract should fail with "binary not found".
	archivePath := filepath.Join(t.TempDir(), "empty.tar.gz")
	makeTarGz(t, archivePath, map[string]string{
		"nvim-linux-x86_64/README.md": "readme",
	})

	_, err := c.Extract(v, p, archivePath)
	if err == nil {
		t.Error("Extract() expected error when binary not in archive")
	}
}
