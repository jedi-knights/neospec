// Package neovim implements ports.NeovimProvider. It downloads Neovim release
// archives from GitHub, caches the extracted binary on disk, and returns the
// path to the binary on each call to Ensure.
package neovim

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jedi-knights/neospec/internal/domain"
)

// Provider satisfies ports.NeovimProvider.
type Provider struct {
	cache      *Cache
	downloader *Downloader
}

// NewProvider creates a Provider that stores binaries under cacheDir.
func NewProvider(cacheDir string) *Provider {
	return &Provider{
		cache:      NewCache(cacheDir),
		downloader: NewDownloader(),
	}
}

// Ensure returns the path to an nvim binary of the requested version.
// It checks the local cache first; on a cache miss it downloads and extracts
// the archive, then caches the result.
func (p *Provider) Ensure(ctx context.Context, version domain.Version, platform domain.Platform) (string, error) {
	binaryPath, ok := p.cache.Lookup(version, platform)
	if ok {
		return binaryPath, nil
	}

	assetName := version.AssetName(platform)
	if assetName == "" {
		return "", fmt.Errorf("unsupported platform: %s", platform)
	}

	archivePath := filepath.Join(p.cache.VersionDir(version, platform), assetName)

	if err := p.downloader.Download(ctx, version, platform, archivePath); err != nil {
		return "", fmt.Errorf("downloading neovim %s for %s: %w", version, platform, err)
	}

	binaryPath, err := p.cache.Extract(version, platform, archivePath)
	if err != nil {
		return "", fmt.Errorf("extracting neovim archive: %w", err)
	}

	return binaryPath, nil
}
