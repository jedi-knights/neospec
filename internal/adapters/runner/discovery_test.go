package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/runner"
)

func TestDiscover_NoMatches(t *testing.T) {
	files, err := runner.Discover(context.Background(), []string{"nonexistent/**/*_spec.lua"})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d: %v", len(files), files)
	}
}

func TestDiscover_FindsFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	specDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	paths := []string{
		filepath.Join(specDir, "a_spec.lua"),
		filepath.Join(specDir, "b_spec.lua"),
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("-- spec"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	pattern := filepath.Join(dir, "test", "*_spec.lua")
	found, err := runner.Discover(context.Background(), []string{pattern})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(found), found)
	}
	for _, f := range found {
		if !filepath.IsAbs(f) {
			t.Errorf("expected absolute path, got %q", f)
		}
	}
}

func TestDiscover_InvalidPattern(t *testing.T) {
	// "[" is an invalid glob pattern — Glob returns an error.
	_, err := runner.Discover(context.Background(), []string{"["})
	if err == nil {
		t.Error("Discover() expected error for invalid glob pattern")
	}
}

func TestDiscover_DeduplicatesPatterns(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "a_spec.lua")
	if err := os.WriteFile(specPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pattern := filepath.Join(dir, "*_spec.lua")
	// Pass the same pattern twice — should only return the file once.
	found, err := runner.Discover(context.Background(), []string{pattern, pattern})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("expected 1 file (deduplicated), got %d: %v", len(found), found)
	}
}
