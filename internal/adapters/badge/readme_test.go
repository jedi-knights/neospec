package badge_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/badge"
)

func TestPatcher_ReplacesBadge(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")

	initial := `# MyPlugin

![coverage](https://img.shields.io/badge/coverage-45.0%25-orange)

Some text.
`
	if err := os.WriteFile(readme, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	p := badge.NewPatcher()
	if err := p.Patch(context.Background(), readme, 92.0); err != nil {
		t.Fatalf("Patch() error: %v", err)
	}

	data, _ := os.ReadFile(readme)
	if strings.Contains(string(data), "45.0") {
		t.Error("old coverage percentage still present after patch")
	}
	if !strings.Contains(string(data), "92.0") {
		t.Error("new coverage percentage not found after patch")
	}
	if !strings.Contains(string(data), "brightgreen") {
		t.Error("expected 'brightgreen' badge color for 92%")
	}
}

func TestPatcher_NoOpWhenNoBadge(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")

	original := "# MyPlugin\n\nNo badge here.\n"
	if err := os.WriteFile(readme, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	p := badge.NewPatcher()
	if err := p.Patch(context.Background(), readme, 80.0); err != nil {
		t.Fatalf("Patch() error: %v", err)
	}

	data, _ := os.ReadFile(readme)
	if string(data) != original {
		t.Error("file was modified despite having no badge")
	}
}
