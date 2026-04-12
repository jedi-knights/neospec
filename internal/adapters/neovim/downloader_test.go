package neovim

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

// errorBodyReader is an io.Reader that always returns an error on Read.
// It is used to simulate a response body that fails during io.Copy.
type errorBodyReader struct{}

func (errorBodyReader) Read(_ []byte) (int, error) {
	return 0, fmt.Errorf("simulated body read failure")
}

// errorBodyTransport is an http.RoundTripper that returns a 200 OK response
// with a body reader that always fails. Used to exercise the io.Copy error path.
type errorBodyTransport struct{}

func (errorBodyTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errorBodyReader{}),
	}, nil
}

// errorTransport is an http.RoundTripper that always returns a network error.
type errorTransport struct{}

func (t *errorTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("simulated network error")
}

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

func TestDownloader_Download_MkdirError(t *testing.T) {
	// A regular file used as a directory component causes os.MkdirAll to fail.
	blocker, err := os.CreateTemp("", "block-dir-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	blocker.Close()
	t.Cleanup(func() { os.Remove(blocker.Name()) })

	d := NewDownloader()
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	// destPath parent = blocker.Name() which is a file, not a directory.
	destPath := filepath.Join(blocker.Name(), "archive.tar.gz")
	err = d.Download(context.Background(), v, p, destPath)
	if err == nil {
		t.Error("Download() expected error when parent dir is a file")
	}
}

func TestDownloader_Download_NetworkError(t *testing.T) {
	d := &Downloader{client: &http.Client{Transport: &errorTransport{}}}
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	err := d.Download(context.Background(), v, p, filepath.Join(t.TempDir(), "nvim.tar.gz"))
	if err == nil {
		t.Error("Download() expected error on network failure")
	}
}

func TestDownloader_Download_CreateFileError(t *testing.T) {
	// Server returns 200 but destPath is an existing directory — os.Create fails.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data"))
	}))
	defer ts.Close()

	d := testDownloader(t, ts)
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	// Passing an existing directory as destPath causes os.Create to fail.
	destPath := t.TempDir()
	err := d.Download(context.Background(), v, p, destPath)
	if err == nil {
		t.Error("Download() expected error when destPath is a directory")
	}
}

// TestDownloader_Download_CopyError covers the io.Copy error path in Download.
// The transport returns a 200 OK with a body reader that always fails,
// causing io.Copy to return an error after os.Create succeeds.
func TestDownloader_Download_CopyError(t *testing.T) {
	d := &Downloader{client: &http.Client{Transport: errorBodyTransport{}}}
	v, _ := domain.ParseVersion("stable")
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}

	err := d.Download(context.Background(), v, p, filepath.Join(t.TempDir(), "out.tar.gz"))
	if err == nil {
		t.Error("Download() expected error when response body copy fails")
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
