package runner_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/runner"
)

// cancelAfterNChecks is a test-only context wrapper that returns nil from
// Err() for the first n calls, then returns context.Canceled. Done() returns
// a channel that is closed at the same moment Err() first returns non-nil, so
// the helper intercepts both ctx.Err() and ctx.Done() based cancellation checks.
type cancelAfterNChecks struct {
	context.Context
	mu     sync.Mutex
	calls  int
	n      int
	done   chan struct{}
	closed bool
}

// newCancelAfterN constructs a cancelAfterNChecks backed by parent. Using
// the constructor ensures the done channel is initialised and prevents
// accidental nil-Context panics on Deadline/Value.
func newCancelAfterN(parent context.Context, n int) *cancelAfterNChecks {
	return &cancelAfterNChecks{
		Context: parent,
		n:       n,
		done:    make(chan struct{}),
	}
}

func (c *cancelAfterNChecks) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls > c.n {
		if !c.closed {
			close(c.done)
			c.closed = true
		}
		return context.Canceled
	}
	return nil
}

// Done returns a channel that is closed the first time Err() returns
// context.Canceled, making the helper correct for select-on-Done patterns.
func (c *cancelAfterNChecks) Done() <-chan struct{} {
	return c.done
}

func (c *cancelAfterNChecks) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestDiscover_PatternEdgeCases covers patterns that produce no results, an
// immediate error, or require no filesystem setup.
func TestDiscover_PatternEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "nil patterns",
			patterns: nil,
			wantLen:  0,
		},
		{
			name:     "empty patterns",
			patterns: []string{},
			wantLen:  0,
		},
		{
			name:     "no matches",
			patterns: []string{"nonexistent/**/*_spec.lua"},
			wantLen:  0,
		},
		{
			name:     "invalid pattern",
			patterns: []string{"["},
			wantErr:  true,
		},
		{
			name:     "multiple double-star segments",
			patterns: []string{"tests/**/fixtures/**/*_spec.lua"},
			wantErr:  true,
		},
		{
			name:     "invalid pattern in double-star suffix with non-existent base",
			patterns: []string{"nonexistent/**/*[invalid"},
			// Non-existent base → ErrNotExist is swallowed → no matches, no error.
			// filepath.Match is never called so the malformed suffix is not surfaced.
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := runner.Discover(context.Background(), tt.patterns)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(files) != tt.wantLen {
				t.Errorf("expected %d files, got %d: %v", tt.wantLen, len(files), files)
			}
		})
	}
}

func TestDiscover_FindsFiles(t *testing.T) {
	dir := t.TempDir()

	specDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	wantPaths := []string{
		filepath.Join(specDir, "a_spec.lua"),
		filepath.Join(specDir, "b_spec.lua"),
	}
	for _, p := range wantPaths {
		if err := os.WriteFile(p, []byte("-- spec"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	pattern := filepath.Join(dir, "test", "*_spec.lua")
	found, err := runner.Discover(context.Background(), []string{pattern})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != len(wantPaths) {
		t.Errorf("expected %d files, got %d: %v", len(wantPaths), len(found), found)
	}

	wantSet := make(map[string]struct{}, len(wantPaths))
	for _, p := range wantPaths {
		wantSet[p] = struct{}{}
	}
	foundSet := make(map[string]struct{}, len(found))
	for _, f := range found {
		if !filepath.IsAbs(f) {
			t.Errorf("expected absolute path, got %q", f)
		}
		if _, ok := wantSet[f]; !ok {
			t.Errorf("unexpected file in results: %q", f)
		}
		foundSet[f] = struct{}{}
	}
	// Reverse check: every expected path must appear in results.
	for _, p := range wantPaths {
		if _, ok := foundSet[p]; !ok {
			t.Errorf("expected %q in results but it was missing", p)
		}
	}
}

// TestDiscover_RelativePattern verifies that filepath.Abs is applied to
// results from non-** patterns, converting relative matches to absolute paths.
// Uses filepath.Rel to build a relative pattern rather than os.Chdir so that
// no global process state is mutated.
func TestDiscover_RelativePattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a_spec.lua"), []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	relDir, err := filepath.Rel(cwd, dir)
	if err != nil {
		t.Skipf("cannot form relative path from cwd to temp dir: %v", err)
	}

	found, err := runner.Discover(context.Background(), []string{filepath.Join(relDir, "*_spec.lua")})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(found), found)
	}
	if !filepath.IsAbs(found[0]) {
		t.Errorf("expected absolute path, got %q", found[0])
	}
}

func TestDiscover_DoubleStarFindsFilesAtAllDepths(t *testing.T) {
	dir := t.TempDir()

	// Create spec files at the root level, one level deep, and two levels deep.
	// Also create a non-matching file to confirm the pattern filters correctly.
	paths := []struct {
		rel   string
		match bool
	}{
		{"a_spec.lua", true},            // directly under root — previously missed
		{"sub/b_spec.lua", true},        // one level deep
		{"sub/nested/c_spec.lua", true}, // two levels deep
		{"other/d_test.lua", false},     // wrong suffix, must not match
	}
	for _, p := range paths {
		full := filepath.Join(dir, filepath.FromSlash(p.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte("-- spec"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	// Build expected and forbidden sets from the table so the match field
	// actually drives the assertions.
	wantSet := make(map[string]struct{})
	forbidSet := make(map[string]struct{})
	for _, p := range paths {
		abs := filepath.Join(dir, filepath.FromSlash(p.rel))
		if p.match {
			wantSet[abs] = struct{}{}
		} else {
			forbidSet[abs] = struct{}{}
		}
	}

	pattern := filepath.Join(dir, "**", "*_spec.lua")
	found, err := runner.Discover(context.Background(), []string{pattern})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != len(wantSet) {
		t.Errorf("expected %d files (all depths), got %d: %v", len(wantSet), len(found), found)
	}
	foundSet := make(map[string]struct{}, len(found))
	for _, f := range found {
		if !filepath.IsAbs(f) {
			t.Errorf("expected absolute path, got %q", f)
		}
		if _, ok := wantSet[f]; !ok {
			t.Errorf("unexpected file in results: %q", f)
		}
		if _, ok := forbidSet[f]; ok {
			t.Errorf("forbidden file returned: %q", f)
		}
		foundSet[f] = struct{}{}
	}
	// Reverse check: every expected path must appear in results.
	for abs := range wantSet {
		if _, ok := foundSet[abs]; !ok {
			t.Errorf("expected %q in results but it was missing", abs)
		}
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

func TestDiscover_CancelledContext(t *testing.T) {
	dir := t.TempDir()
	// Create some spec files so the pattern would match if context were ignored.
	for _, name := range []string{"a_spec.lua", "b_spec.lua"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("-- spec"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before any work starts — exercises the outer loop guard

	_, err := runner.Discover(ctx, []string{filepath.Join(dir, "**", "*_spec.lua")})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestDiscover_CancelledContextDuringWalk exercises the ctx.Err() check
// inside the WalkDir callback, not the outer loop guard. newCancelAfterN
// allows the outer check to pass (n=1) and then returns context.Canceled on
// the first callback invocation, making this deterministic without goroutines.
// The call count is asserted post-call to confirm the walk was actually entered.
func TestDiscover_CancelledContextDuringWalk(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a_spec.lua", "b_spec.lua"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("-- spec"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	// n=1: the outer loop calls Err() once (returns nil), then the first
	// WalkDir callback invocation (the base directory itself) returns context.Canceled.
	ctx := newCancelAfterN(context.Background(), 1)
	_, err := runner.Discover(ctx, []string{filepath.Join(dir, "**", "*_spec.lua")})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from mid-walk cancellation, got %v", err)
	}
	// Confirm at least two Err() calls occurred: one for the outer loop guard
	// and one (or more) inside the WalkDir callback. This proves cancellation
	// was returned from the walk path, not from the outer loop alone.
	if got := ctx.callCount(); got < 2 {
		t.Errorf("expected ctx.Err() called at least twice (outer loop + walk callback), got %d", got)
	}
}

func TestDiscover_InvalidDoubleStarSuffix(t *testing.T) {
	dir := t.TempDir()
	// A matching file must exist so WalkDir calls filepath.Match and returns ErrBadPattern.
	if err := os.WriteFile(filepath.Join(dir, "a_spec.lua"), []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// "*[invalid" has an unclosed character class — filepath.Match returns ErrBadPattern.
	pattern := filepath.Join(dir, "**", "*[invalid")
	_, err := runner.Discover(context.Background(), []string{pattern})
	if err == nil {
		t.Error("expected error for invalid glob pattern in double-star suffix, got nil")
	}
}

func TestDiscover_SkipsUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 not meaningful on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; permission restrictions do not apply")
	}

	dir := t.TempDir()

	// Create a readable sub-directory with a spec file.
	readableDir := filepath.Join(dir, "readable")
	if err := os.MkdirAll(readableDir, 0o755); err != nil {
		t.Fatalf("MkdirAll readable: %v", err)
	}
	specPath := filepath.Join(readableDir, "a_spec.lua")
	if err := os.WriteFile(specPath, []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create an unreadable sub-directory; files inside are silently skipped.
	unreadableDir := filepath.Join(dir, "unreadable")
	if err := os.MkdirAll(unreadableDir, 0o755); err != nil {
		t.Fatalf("MkdirAll unreadable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unreadableDir, "b_spec.lua"), []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(unreadableDir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(unreadableDir, 0o755); err != nil {
			t.Logf("cleanup Chmod: %v", err)
		}
	})

	pattern := filepath.Join(dir, "**", "*_spec.lua")
	found, err := runner.Discover(context.Background(), []string{pattern})
	if err != nil {
		t.Fatalf("Discover() unexpected error: %v", err)
	}
	// Files in the readable directory should be found; the unreadable one silently skipped.
	if len(found) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(found), found)
	}
	if found[0] != specPath {
		t.Errorf("expected %q, got %q", specPath, found[0])
	}
}

func TestDiscover_UnreadableRootDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 not meaningful on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; permission restrictions do not apply")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a_spec.lua"), []byte("-- spec"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Logf("cleanup Chmod: %v", err)
		}
	})

	pattern := filepath.Join(dir, "**", "*_spec.lua")
	_, err := runner.Discover(context.Background(), []string{pattern})
	if err == nil {
		t.Error("Discover() expected error for unreadable root directory, got nil")
	}
}

// TestDiscover_BareDoubleStarMatchesAllFiles verifies the documented behaviour
// that a bare "**" with no trailing segment matches all non-directory files
// under the base directory (the empty filePat is coerced to "*").
func TestDiscover_BareDoubleStarMatchesAllFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a_spec.lua", "b_spec.lua", "readme.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	pattern := filepath.Join(dir, "**")
	found, err := runner.Discover(context.Background(), []string{pattern})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(found), found)
	}
	for _, f := range found {
		if !filepath.IsAbs(f) {
			t.Errorf("expected absolute path, got %q", f)
		}
	}
}

// TestDiscover_DeduplicatesPatternsDoublestar verifies that the deduplication
// map in Discover collapses duplicate results from globDoublestar, not just
// from filepath.Glob.
func TestDiscover_DeduplicatesPatternsDoublestar(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "a_spec.lua")
	if err := os.WriteFile(specPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Pass the same ** pattern twice — should return the file exactly once.
	pattern := filepath.Join(dir, "**", "*_spec.lua")
	found, err := runner.Discover(context.Background(), []string{pattern, pattern})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("expected 1 file (deduplicated), got %d: %v", len(found), found)
	}
}

func BenchmarkDiscover_DoublestarWalk(b *testing.B) {
	dir := b.TempDir()
	// Create a moderate directory tree: 3 sub-directories, each with 5 nested
	// leaves, each containing one spec file — 15 matches total.
	for i := range 3 {
		for j := range 5 {
			leaf := filepath.Join(dir, fmt.Sprintf("sub%d", i), fmt.Sprintf("leaf%d", j))
			if err := os.MkdirAll(leaf, 0o755); err != nil {
				b.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(filepath.Join(leaf, "spec_test.lua"), []byte("-- spec"), 0o644); err != nil {
				b.Fatalf("WriteFile: %v", err)
			}
		}
	}
	pattern := filepath.Join(dir, "**", "*_test.lua")
	b.ResetTimer()
	for range b.N {
		_, err := runner.Discover(context.Background(), []string{pattern})
		if err != nil {
			b.Fatal(err)
		}
	}
}
