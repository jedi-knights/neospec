package cover

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLua(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestFunctionNameMapEmpty(t *testing.T) {
	// Nil input returns nil (not an empty map) so the runner can skip
	// emitting the Lua global entirely rather than emitting `= {}`.
	if got := FunctionNameMap(nil); got != nil {
		t.Errorf("FunctionNameMap(nil) = %v, want nil", got)
	}
	if got := FunctionNameMap([]string{}); got != nil {
		t.Errorf("FunctionNameMap(empty) = %v, want nil", got)
	}
}

func TestFunctionNameMapExtractsAcrossFiles(t *testing.T) {
	a := writeLua(t, "a.lua", "function M.foo() end\nlocal function bar() end")
	b := writeLua(t, "b.lua", "M.baz = function() end")

	got := FunctionNameMap([]string{a, b})
	if len(got) != 2 {
		t.Fatalf("map size = %d, want 2", len(got))
	}
	if got[a][1] != "M.foo" {
		t.Errorf("a:1 = %q, want M.foo", got[a][1])
	}
	if got[a][2] != "bar" {
		t.Errorf("a:2 = %q, want bar", got[a][2])
	}
	if got[b][1] != "M.baz" {
		t.Errorf("b:1 = %q, want M.baz", got[b][1])
	}
}

func TestFunctionNameMapMissingFileSkipped(t *testing.T) {
	// Non-existent paths are silently skipped — same policy as
	// PopulateBranches. Callers pass whatever the glob walk produced;
	// a stale entry should not abort the whole map.
	got := FunctionNameMap([]string{"/nonexistent/does-not-exist.lua"})
	if len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %v", got)
	}
}

func TestFunctionNameMapNoFunctionsSkipped(t *testing.T) {
	// A file with no function definitions is absent from the output
	// (not present with an empty inner map). Keeps the emitted Lua
	// global small and lets the coverage hook fall through to its
	// pattern-based fallback for anything not in the map.
	path := writeLua(t, "plain.lua", "local x = 1\nreturn x")
	got := FunctionNameMap([]string{path})
	if _, present := got[path]; present {
		t.Errorf("plain file should be absent from map, got entry: %v", got[path])
	}
}

func TestFunctionNameMapLastWinsOnLineCollision(t *testing.T) {
	// Two functions on the same source line collide in the returned
	// map — debug.getinfo can't tell them apart by line, so a single
	// label per line matches the runtime hook's resolution.
	path := writeLua(t, "collide.lua",
		"local f, g = function() end, function() end")
	got := FunctionNameMap([]string{path})
	if len(got[path]) != 1 {
		t.Fatalf("expected 1 entry (last-wins), got %d: %v", len(got[path]), got[path])
	}
	if got[path][1] != "g" {
		t.Errorf("line 1 = %q, want g (last-wins on collision)", got[path][1])
	}
}
