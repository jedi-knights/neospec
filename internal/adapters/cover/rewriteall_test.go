package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLuaSrc(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRewriteAllEmpty(t *testing.T) {
	// Nil input returns (nil, nil) so the runner can skip emitting the
	// global entirely — not (empty-map, nil-slice) which would look
	// like "was configured but zero branches".
	sources, injections := RewriteAll(nil)
	if sources != nil || injections != nil {
		t.Errorf("RewriteAll(nil) = (%v, %v), want (nil, nil)", sources, injections)
	}
}

func TestRewriteAllUniqueIDsAcrossFiles(t *testing.T) {
	// Multi-file run: BranchIDs must not collide across files. If they
	// did, ApplyBranchCounters would silently attribute one file's hits
	// to another file's arm.
	dir := t.TempDir()
	a := writeLuaSrc(t, dir, "a.lua", "if x then A() end")
	b := writeLuaSrc(t, dir, "b.lua", "if y then B() end")

	_, injections := RewriteAll([]string{a, b})
	if len(injections) < 2 {
		t.Fatalf("expected at least 2 injections, got %d", len(injections))
	}
	seen := map[int]bool{}
	for _, inj := range injections {
		if seen[inj.BranchID] {
			t.Errorf("duplicate BranchID %d across files", inj.BranchID)
		}
		seen[inj.BranchID] = true
	}
}

func TestRewriteAllRewrittenSourceIncludesInjection(t *testing.T) {
	dir := t.TempDir()
	path := writeLuaSrc(t, dir, "hit.lua", "if x then A() end")

	sources, injections := RewriteAll([]string{path})
	if len(sources) != 1 || len(injections) != 1 {
		t.Fatalf("expected 1 source + 1 injection, got %d sources / %d injections",
			len(sources), len(injections))
	}
	if !strings.Contains(sources[path], "_neospec_br(") {
		t.Errorf("rewritten source missing counter call:\n%s", sources[path])
	}
	// Injection metadata must point back at the same file so
	// ApplyBranchCounters can look it up in CoverageData.
	if injections[0].File != path {
		t.Errorf("injection File = %q, want %q", injections[0].File, path)
	}
}

func TestRewriteAllSkipsFilesWithNoBranches(t *testing.T) {
	// A straight-line file has nothing to rewrite. Skipping the entry
	// keeps the payload small — the harness's default loader handles
	// these files normally.
	dir := t.TempDir()
	plain := writeLuaSrc(t, dir, "plain.lua", "local x = 1\nreturn x")
	branchy := writeLuaSrc(t, dir, "branchy.lua", "while x do A() end")

	sources, _ := RewriteAll([]string{plain, branchy})
	if _, present := sources[plain]; present {
		t.Errorf("plain file should be absent from sources map")
	}
	if _, present := sources[branchy]; !present {
		t.Errorf("branchy file should be in sources map, got: %v", sources)
	}
}

func TestRewriteAllSkipsMissingFiles(t *testing.T) {
	// Non-existent paths silently skipped — same policy as
	// PopulateBranches / FunctionNameMap. Stale glob entries should not
	// abort the whole rewrite.
	sources, injections := RewriteAll([]string{"/nonexistent/x.lua"})
	if sources != nil || injections != nil {
		t.Errorf("RewriteAll(missing) = (%v, %v), want (nil, nil)", sources, injections)
	}
}
