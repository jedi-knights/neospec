// White-box tests for unexported cache helpers.
package neovim

import (
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
