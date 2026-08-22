package cover

import (
	"strings"
	"testing"
)

func TestBuildShim_PlenaryBusted(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:       RunnerPlenaryBusted,
		Dir:        "tests/",
		OutputFile: "/tmp/out.json",
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	s := string(shim)
	for _, want := range []string{
		"debug.sethook",                   // hook installed
		"_neospec_report",                 // reporter loaded
		"VimLeavePre",                     // exit-time autocmd
		`io.open("/tmp/out.json"`,         // output file wired
		"plenary.test_harness",            // plenary invoked
		`harness.test_directory("tests/"`, // dir escaped and inlined
		`vim.cmd("qa!")`,                  // explicit exit
	} {
		if !strings.Contains(s, want) {
			t.Errorf("shim missing %q\n---\n%s", want, s)
		}
	}
}

func TestBuildShim_MiniTest(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:       RunnerMiniTest,
		Dir:        "tests/**/*_test.lua",
		OutputFile: "/tmp/out.json",
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	s := string(shim)
	for _, want := range []string{
		"mini.test",
		`minitest.run`,
		`vim.fn.glob("tests/**/*_test.lua")`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("shim missing %q\n---\n%s", want, s)
		}
	}
}

func TestBuildShim_ExternalRejected(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerExternal, OutputFile: "/tmp/out.json"})
	if err == nil || !strings.Contains(err.Error(), "external mode does not use a shim") {
		t.Errorf("want external-rejection error, got: %v", err)
	}
}

func TestBuildShim_UnknownMode(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerMode("foo"), OutputFile: "/tmp/out.json"})
	if err == nil || !strings.Contains(err.Error(), "unknown runner mode") {
		t.Errorf("want unknown-mode error, got: %v", err)
	}
}

func TestBuildShim_MissingOutputFile(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerPlenaryBusted, Dir: "tests/"})
	if err == nil || !strings.Contains(err.Error(), "output file must not be empty") {
		t.Errorf("want missing-output error, got: %v", err)
	}
}

func TestBuildShim_MissingDir(t *testing.T) {
	_, err := BuildShim(ShimOpts{Mode: RunnerPlenaryBusted, OutputFile: "/tmp/out.json"})
	if err == nil || !strings.Contains(err.Error(), "requires --dir") {
		t.Errorf("want missing-dir error, got: %v", err)
	}
}

func TestBuildShim_NULRejected(t *testing.T) {
	cases := []struct {
		name string
		opts ShimOpts
	}{
		{"NUL in output", ShimOpts{Mode: RunnerPlenaryBusted, Dir: "tests/", OutputFile: "/tmp/\x00out.json"}},
		{"NUL in dir", ShimOpts{Mode: RunnerPlenaryBusted, Dir: "tests/\x00foo", OutputFile: "/tmp/out.json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildShim(tc.opts)
			if err == nil || !strings.Contains(err.Error(), "NUL") {
				t.Errorf("want NUL-rejection error, got: %v", err)
			}
		})
	}
}

func TestLuaEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`plain`, `plain`},
		{`with"quote`, `with\"quote`},
		{`with\back`, `with\\back`},
		{"with\nnl", `with\nnl`},
		{"with\ttab", `with\ttab`},
		{`combo"\`, `combo\"\\`},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := luaEscape(tc.in); got != tc.want {
				t.Errorf("luaEscape(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildShim_EmitsCoverageSourcesWhenPresent pins that the resolved
// coverage-source list lands as a Lua global sorted deterministically.
// The coverage_hook's finalizer keys on this global to add zero-count
// entries for files no wrapped test loaded.
func TestBuildShim_EmitsCoverageSourcesWhenPresent(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:            RunnerPlenaryBusted,
		Dir:             "tests/",
		OutputFile:      "/tmp/out.json",
		CoverageSources: []string{"/src/b.lua", "/src/a.lua"},
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	got := string(shim)
	if !strings.Contains(got, "_neospec_coverage_sources = {") {
		t.Errorf("global assignment missing:\n%s", got)
	}
	// Sorted: a.lua must appear before b.lua for reproducibility.
	aIdx := strings.Index(got, `"/src/a.lua"`)
	bIdx := strings.Index(got, `"/src/b.lua"`)
	if aIdx == -1 || bIdx == -1 || aIdx > bIdx {
		t.Errorf("paths out of sorted order (a=%d b=%d):\n%s", aIdx, bIdx, got)
	}
	// Emitted BEFORE the coverage hook loads so _neospec_coverage_finalize
	// picks it up. debug.sethook is inside the hook, so the sources
	// global must precede it.
	sourcesIdx := strings.Index(got, "_neospec_coverage_sources = {")
	hookIdx := strings.Index(got, "debug.sethook")
	if sourcesIdx == -1 || hookIdx == -1 || sourcesIdx > hookIdx {
		t.Errorf("sources global must be emitted before the hook (sources=%d hook=%d)", sourcesIdx, hookIdx)
	}
}

// TestBuildShim_NoCoverageSourcesEmitsNothing pins the fallback path:
// nil / empty input produces no global so the hook's default behavior
// (record only files a test loaded) runs unchanged.
func TestBuildShim_NoCoverageSourcesEmitsNothing(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:       RunnerPlenaryBusted,
		Dir:        "tests/",
		OutputFile: "/tmp/out.json",
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	if strings.Contains(string(shim), "_neospec_coverage_sources = {") {
		t.Errorf("global should be absent for nil sources:\n%s", shim)
	}
}

// TestBuildShim_CoverageSourcesEscapesSpecialChars pins that path
// strings go through luaEscape — a filename with a quote or newline
// would otherwise produce broken Lua that LuaJIT rejects with an
// opaque parse error.
func TestBuildShim_CoverageSourcesEscapesSpecialChars(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:            RunnerPlenaryBusted,
		Dir:             "tests/",
		OutputFile:      "/tmp/out.json",
		CoverageSources: []string{`/src/weird"path.lua`},
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	if !strings.Contains(string(shim), `\"path.lua`) {
		t.Errorf("path double-quote not escaped:\n%s", shim)
	}
}

// TestBuildShim_EmitsCoverageIncludeWhenPresent pins that the
// coverage-include substring list lands as a Lua global in the user's
// given order. Order is preserved (not sorted) because the hook
// short-circuits on the first match — reorder changes the fast path
// but never the result set.
func TestBuildShim_EmitsCoverageIncludeWhenPresent(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:            RunnerPlenaryBusted,
		Dir:             "tests/",
		OutputFile:      "/tmp/out.json",
		CoverageInclude: []string{"lua/", "plugin/"},
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	got := string(shim)
	if !strings.Contains(got, `_neospec_coverage_include = {"lua/", "plugin/"}`) {
		t.Errorf("global emitted incorrectly:\n%s", got)
	}
	// Emitted BEFORE the coverage hook loads so is_project_source sees
	// the global on every line event.
	includeIdx := strings.Index(got, "_neospec_coverage_include = {")
	hookIdx := strings.Index(got, "debug.sethook")
	if includeIdx == -1 || hookIdx == -1 || includeIdx > hookIdx {
		t.Errorf("include global must be emitted before the hook (include=%d hook=%d)", includeIdx, hookIdx)
	}
}

// TestBuildShim_NoCoverageIncludeEmitsNothing pins the default path:
// no filter emits no global, so the coverage hook records every file
// the wrapped runner touches (Neovim runtime included). Matches the
// run command's behavior.
func TestBuildShim_NoCoverageIncludeEmitsNothing(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:       RunnerPlenaryBusted,
		Dir:        "tests/",
		OutputFile: "/tmp/out.json",
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	if strings.Contains(string(shim), "_neospec_coverage_include = {") {
		t.Errorf("global should be absent for nil include:\n%s", shim)
	}
}

// TestBuildShim_CoverageIncludeEscapesSpecialChars pins escape safety
// for substrings — a pattern with a quote (rare, possible on
// filesystems with unusual paths) must be escaped or LuaJIT rejects
// the shim.
func TestBuildShim_CoverageIncludeEscapesSpecialChars(t *testing.T) {
	shim, err := BuildShim(ShimOpts{
		Mode:            RunnerPlenaryBusted,
		Dir:             "tests/",
		OutputFile:      "/tmp/out.json",
		CoverageInclude: []string{`weird"pattern`},
	})
	if err != nil {
		t.Fatalf("BuildShim: %v", err)
	}
	if !strings.Contains(string(shim), `\"pattern`) {
		t.Errorf("include pattern double-quote not escaped:\n%s", shim)
	}
}
