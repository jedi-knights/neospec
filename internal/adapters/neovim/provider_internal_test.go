// White-box tests for Provider that require access to unexported fields.
package neovim

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

// makeTarGzBytes builds a tar.gz in memory for use in provider tests.
func makeTarGzBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "*.tar.gz")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer tmp.Close()

	gw := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gw)
	for name, content := range entries {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: int64(len(content)), Mode: 0o755})
		tw.Write([]byte(content))
	}
	tw.Close()
	gw.Close()

	data, _ := os.ReadFile(tmp.Name())
	return data
}

func TestProvider_Ensure_DownloadError(t *testing.T) {
	// Server returns 404 — Download() returns an error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cacheDir := t.TempDir()
	tsURL, _ := url.Parse(ts.URL)

	p := &Provider{
		cache:      NewCache(cacheDir),
		downloader: &Downloader{client: &http.Client{Transport: &rewriteTransport{target: tsURL}}},
	}

	v, _ := domain.ParseVersion("stable")
	platform := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	_, err := p.Ensure(context.Background(), v, platform)
	if err == nil {
		t.Error("Ensure() expected error on download failure, got nil")
	}
}

func TestProvider_Ensure_ExtractError(t *testing.T) {
	// Server returns 200 but with invalid archive data — Extract() returns an error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("this is not a valid archive"))
	}))
	defer ts.Close()

	cacheDir := t.TempDir()
	tsURL, _ := url.Parse(ts.URL)

	p := &Provider{
		cache:      NewCache(cacheDir),
		downloader: &Downloader{client: &http.Client{Transport: &rewriteTransport{target: tsURL}}},
	}

	v, _ := domain.ParseVersion("stable")
	platform := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	_, err := p.Ensure(context.Background(), v, platform)
	if err == nil {
		t.Error("Ensure() expected error on extract failure, got nil")
	}
}

func TestProvider_Ensure_DownloadAndExtract(t *testing.T) {
	archiveData := makeTarGzBytes(t, map[string]string{
		"nvim-linux-x86_64/bin/nvim": "fake nvim binary",
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(archiveData)
	}))
	defer ts.Close()

	cacheDir := t.TempDir()
	tsURL, _ := url.Parse(ts.URL)

	// Build provider with custom downloader pointing at test server.
	p := &Provider{
		cache:      NewCache(cacheDir),
		downloader: &Downloader{client: &http.Client{Transport: &rewriteTransport{target: tsURL}}},
	}

	v, _ := domain.ParseVersion("stable")
	platform := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	binPath, err := p.Ensure(context.Background(), v, platform)
	if err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Errorf("binary not found at %q: %v", binPath, err)
	}
}
