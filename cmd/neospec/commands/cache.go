package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jedi-knights/neospec/internal/config"
)

// NewCacheCmd returns the `neospec cache` parent command with subcommands.
func NewCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the Neovim binary cache",
	}
	cmd.AddCommand(newCacheCleanCmd(), newCacheListCmd())
	return cmd
}

func newCacheCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Remove all cached Neovim binaries",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _ := config.Load("neospec.toml")
			if err := os.RemoveAll(cfg.CacheDir); err != nil {
				return fmt.Errorf("cleaning cache: %w", err)
			}
			fmt.Printf("Removed cache directory: %s\n", cfg.CacheDir)
			return nil
		},
	}
}

func newCacheListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cached Neovim versions and their sizes",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _ := config.Load("neospec.toml")
			return listCache(cfg.CacheDir)
		},
	}
}

func listCache(cacheDir string) error {
	entries, err := os.ReadDir(cacheDir)
	if os.IsNotExist(err) {
		fmt.Println("Cache is empty.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading cache dir: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("Cache is empty.")
		return nil
	}

	fmt.Printf("%-20s  %s\n", "VERSION", "SIZE")
	fmt.Printf("%-20s  %s\n", "-------", "----")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		size, _ := dirSize(filepath.Join(cacheDir, e.Name()))
		fmt.Printf("%-20s  %s\n", e.Name(), formatBytes(size))
	}
	return nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
