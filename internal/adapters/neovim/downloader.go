package neovim

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jedi-knights/neospec/internal/domain"
)

// githubReleaseBase is the base URL for Neovim's GitHub releases.
const githubReleaseBase = "https://github.com/neovim/neovim/releases/download"

// Downloader fetches Neovim release archives from GitHub.
type Downloader struct {
	client *http.Client
}

// NewDownloader creates a Downloader with the default HTTP client.
func NewDownloader() *Downloader {
	return &Downloader{client: &http.Client{}}
}

// Download fetches the release archive for version+platform and writes it to
// destPath, creating parent directories as needed.
func (d *Downloader) Download(ctx context.Context, v domain.Version, p domain.Platform, destPath string) (retErr error) {
	assetName := v.AssetName(p)
	if assetName == "" {
		return fmt.Errorf("no asset defined for platform %s", p)
	}

	url := fmt.Sprintf("%s/%s/%s", githubReleaseBase, v.Tag, assetName)

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("creating download dir: %w", err)
	}

	// This error branch is structurally unreachable: http.NewRequestWithContext
	// only fails when the method or URL is malformed. Both are constructed from
	// the compile-time constant http.MethodGet and a well-formed URL built from
	// the fixed githubReleaseBase prefix. No user-supplied input reaches here.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP %d fetching %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing archive: %w", err)
	}
	return nil
}
