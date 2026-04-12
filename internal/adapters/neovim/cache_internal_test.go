// White-box tests for unexported cache helpers.
package neovim

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_OpenError(t *testing.T) {
	// Passing a path whose parent directory does not exist causes os.OpenFile to fail.
	nonexistentDir := filepath.Join(t.TempDir(), "does-not-exist")
	err := writeFile(filepath.Join(nonexistentDir, "out.txt"), strings.NewReader("data"), 0o644)
	if err == nil {
		t.Error("writeFile() expected error for path in nonexistent directory")
	}
}

// TestExtractTarGz_CorruptTarStream tests the "reading tar entry" error path by
// providing a file that is valid gzip but contains fewer bytes than a tar header
// block (512 bytes). The tar reader returns io.ErrUnexpectedEOF rather than io.EOF.
// TestExtractTarGz_TypeDir_MkdirAllError covers the os.MkdirAll error inside
// the TypeDir case. A regular file "blocker" is created first, then a directory
// entry "blocker/sub/" tries to MkdirAll under it — fails because "blocker" is a file.
func TestExtractTarGz_TypeDir_MkdirAllError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "conflict.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Regular file creates destDir/blocker.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "prefix/blocker", Size: 4, Mode: 0o644})
	tw.Write([]byte("data"))
	// Directory entry tries to MkdirAll destDir/blocker/sub — parent is a file.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "prefix/blocker/sub/", Mode: 0o755})
	tw.Close()
	gw.Close()
	f.Close()

	err = extractTarGz(archivePath, t.TempDir())
	if err == nil {
		t.Error("extractTarGz() expected error when dir MkdirAll fails (parent is a file)")
	}
}

// TestExtractTarGz_TypeReg_MkdirAllError covers the os.MkdirAll error inside
// the TypeReg case. A regular file "blocker" is created first, then a TypeReg
// entry "blocker/child" tries to MkdirAll the parent "blocker" — fails.
func TestExtractTarGz_TypeReg_MkdirAllError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "reg_conflict.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Regular file creates destDir/blocker.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "prefix/blocker", Size: 4, Mode: 0o644})
	tw.Write([]byte("data"))
	// Regular file entry whose parent is the existing file blocker — MkdirAll(destDir/blocker) fails.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "prefix/blocker/child", Size: 4, Mode: 0o644})
	tw.Write([]byte("data"))
	tw.Close()
	gw.Close()
	f.Close()

	err = extractTarGz(archivePath, t.TempDir())
	if err == nil {
		t.Error("extractTarGz() expected error when parent dir MkdirAll fails for TypeReg entry")
	}
}

// TestExtractTarGz_WriteFileError covers the writeFile error return inside the
// TypeReg case. A TypeDir entry "foo/" creates destDir/foo as a directory first,
// then a TypeReg entry "foo" tries to write a file at the same path — fails.
func TestExtractTarGz_WriteFileError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "write_conflict.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Directory entry creates destDir/foo.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "prefix/foo/", Mode: 0o755})
	// Regular file entry at same path — writeFile fails because target is a directory.
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "prefix/foo", Size: 4, Mode: 0o644})
	tw.Write([]byte("data"))
	tw.Close()
	gw.Close()
	f.Close()

	err = extractTarGz(archivePath, t.TempDir())
	if err == nil {
		t.Error("extractTarGz() expected error when writeFile target is an existing directory")
	}
}

// TestExtractZip_DirEntryMkdirAllError covers the os.MkdirAll error in the
// IsDir branch of extractZip. A file entry "blocker" is created first, then a
// directory entry "blocker/sub/" tries MkdirAll under the existing file — fails.
func TestExtractZip_DirEntryMkdirAllError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "dir_conflict.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(f)
	// File entry creates destDir/blocker.
	w, _ := zw.Create("nvim-win64/blocker")
	w.Write([]byte("data"))
	// Directory entry whose MkdirAll parent is the existing file — fails.
	zw.Create("nvim-win64/blocker/sub/")
	zw.Close()
	f.Close()

	err = extractZip(archivePath, t.TempDir())
	if err == nil {
		t.Error("extractZip() expected error when dir entry MkdirAll fails (parent is a file)")
	}
}

// TestExtractZip_FileEntryMkdirAllError covers the os.MkdirAll error in the
// file-entry branch of extractZip. A file entry "blocker" is created first,
// then a file entry "blocker/child" tries MkdirAll("blocker") — fails.
func TestExtractZip_FileEntryMkdirAllError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "file_parent_conflict.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(f)
	// File entry creates destDir/blocker.
	w, _ := zw.Create("nvim-win64/blocker")
	w.Write([]byte("data"))
	// File entry whose parent dir is the existing regular file blocker — MkdirAll fails.
	w2, _ := zw.Create("nvim-win64/blocker/child")
	w2.Write([]byte("data"))
	zw.Close()
	f.Close()

	err = extractZip(archivePath, t.TempDir())
	if err == nil {
		t.Error("extractZip() expected error when file entry parent MkdirAll fails")
	}
}

func TestExtractTarGz_CorruptTarStream(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "short.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gw := gzip.NewWriter(f)
	// Write fewer than 512 bytes — not enough for a tar header block.
	gw.Write([]byte("not enough bytes for a tar header"))
	gw.Close()
	f.Close()

	err = extractTarGz(archivePath, t.TempDir())
	if err == nil {
		t.Error("extractTarGz() expected error for truncated tar stream")
	}
}
