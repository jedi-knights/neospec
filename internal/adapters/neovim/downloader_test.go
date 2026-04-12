package neovim

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

// rewriteTransport redirects all requests to target, preserving the path.
type rewriteTransport struct {
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testDownloader(t *testing.T, ts *httptest.Server) *Downloader {
	t.Helper()
	u, _ := url.Parse(ts.URL)
	return &Downloader{client: &http.Client{Transport: &rewriteTransport{target: u}}}
}

func TestNewDownloader(t *testing.T) {
	d := NewDownloader()
	if d == nil {
		t.Fatal("NewDownloader() returned nil")
	}
}

func TestDownloader_Download_UnsupportedPlatform(t *testing.T) {
	d := NewDownloader()
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OS("unsupported"), Arch: domain.ArchAMD64}

	err := d.Download(context.Background(), v, p, filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Error("Download() expected error for unsupported platform")
	}
}

func TestDownloader_Download_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake archive content"))
	}))
	defer ts.Close()

	d := testDownloader(t, ts)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}
	destPath := filepath.Join(t.TempDir(), "nvim.tar.gz")

	if err := d.Download(context.Background(), v, p, destPath); err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "fake archive content" {
		t.Errorf("downloaded content = %q, want %q", string(data), "fake archive content")
	}
}

func TestDownloader_Download_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := testDownloader(t, ts)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	err := d.Download(context.Background(), v, p, filepath.Join(t.TempDir(), "out.tar.gz"))
	if err == nil {
		t.Error("Download() expected error for HTTP 404")
	}
}
