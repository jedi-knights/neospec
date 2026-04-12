package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.5 GB"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			if got := formatBytes(tc.input); got != tc.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestListCache_Nonexistent(t *testing.T) {
	// A non-existent directory should print "Cache is empty." without error.
	err := listCache("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Errorf("listCache(nonexistent) error: %v", err)
	}
}

func TestListCache_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	err := listCache(dir)
	if err != nil {
		t.Errorf("listCache(empty) error: %v", err)
	}
}

func TestListCache_WithEntries(t *testing.T) {
	dir := t.TempDir()

	// Create version subdirectory with a file inside.
	versionDir := filepath.Join(dir, "stable")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "nvim"), []byte("fake binary"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Also add a non-directory file — it should be skipped.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("readme"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := listCache(dir)
	if err != nil {
		t.Errorf("listCache(with entries) error: %v", err)
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("world!"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	size, err := dirSize(dir)
	if err != nil {
		t.Fatalf("dirSize() error: %v", err)
	}
	if size != 11 { // "hello" (5) + "world!" (6)
		t.Errorf("dirSize() = %d, want 11", size)
	}
}

func TestListCache_ReadDirError(t *testing.T) {
	// Pass a regular file path instead of a directory — ReadDir returns an error
	// that is not os.IsNotExist, so listCache should return a wrapped error.
	tmpFile, err := os.CreateTemp("", "not-a-dir-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	err = listCache(tmpFile.Name())
	if err == nil {
		t.Error("listCache() expected error for non-directory path")
	}
}

func TestNewCacheCmd(t *testing.T) {
	cmd := NewCacheCmd()
	if cmd == nil {
		t.Fatal("NewCacheCmd() returned nil")
	}
	if cmd.Use != "cache" {
		t.Errorf("cmd.Use = %q, want %q", cmd.Use, "cache")
	}
}
