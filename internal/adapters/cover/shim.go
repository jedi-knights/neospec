// Package cover instruments an existing Neovim test runner with coverage
// collection, without replacing the runner. It is the adapter behind
// `neospec cover`, the companion mode that unlocks adoption for users of
// plenary-busted and mini.test who don't want to switch test frameworks.
package cover

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jedi-knights/neospec/internal/adapters/runner"
)

// RunnerMode names one of the supported wrapped-runner shapes. Each mode
// determines how the shim invokes the underlying test framework after the
// coverage hook is installed.
type RunnerMode string

const (
	// RunnerPlenaryBusted runs plenary.busted specs in-process via
	// require("plenary.busted").run per glob-discovered spec. The user's
	// minimal_init file must bootstrap plenary onto the runtimepath.
	// In-process rather than the more obvious test_harness.test_directory
	// (or PlenaryBustedFile, which routes to the same place): those spawn
	// a fresh nvim child per spec, and the coverage hook installed in the
	// parent does not follow into the child.
	RunnerPlenaryBusted RunnerMode = "plenary-busted"
	// RunnerMiniTest wraps mini.test's MiniTest.run. The user's minimal_init
	// file must bootstrap mini.test onto the runtimepath.
	RunnerMiniTest RunnerMode = "mini-test"
	// RunnerExternal defers the runner invocation entirely to the user's own
	// command. The cover adapter sets NEOSPEC_COVER_HOOK and NEOSPEC_COVER_OUTPUT
	// env vars and the user's command is responsible for loading the hook and
	// producing the output file.
	RunnerExternal RunnerMode = "external"
)

// ShimOpts is the input to BuildShim. Callers populate only the fields that
// apply to their RunnerMode; unused fields are ignored.
type ShimOpts struct {
	// Mode selects the shim shape.
	Mode RunnerMode
	// Dir is the test directory or file the wrapped runner scans. Required for
	// plenary-busted and mini-test modes; unused for external mode.
	Dir string
	// OutputFile is the absolute path the reporter should write its JSON output
	// to. Required. The cover executor reads this file after the wrapped runner
	// exits and passes the contents to runner.ParseReporterOutput.
	OutputFile string
	// CoverageSources is a list of already-resolved absolute paths (post-glob)
	// that the coverage hook records at zero when no test loads them. Emitted
	// as `_neospec_coverage_sources = {...}` before the hook runs so the
	// finalizer picks it up. Without this, cover-mode reports silently omit
	// files no wrapped test touched — flattering the percentage on any run
	// whose test suite doesn't reach every module.
	CoverageSources []string
	// CoverageInclude is a substring filter emitted as
	// `_neospec_coverage_include = {...}` before the hook. When non-empty,
	// the coverage hook records only files whose absolute path contains at
	// least one of these substrings — the typical usage is scoping to the
	// plugin's own source tree and excluding Neovim's runtime files.
	//
	// Substrings, not globs: the hook does plain path:find(pattern, 1, true)
	// so a raw pattern like "lua/" matches any path with that substring
	// anywhere. Matches the run command's semantics exactly.
	CoverageInclude []string
	// FunctionNames is an AST-recovered (path, line) → name map emitted as
	// `_neospec_function_names = {...}` before the hook. The coverage hook
	// looks up function names from this global rather than pattern-matching
	// source (NAME_PATTERNS was deleted in #43). Cover mode auto-populates
	// from cover.FunctionNameMap over the resolved coverage sources; any
	// (path, line) not present renders as "anonymous@N" in the report.
	FunctionNames map[string]map[int]string
	// RewrittenSources is a {path -> rewritten_source} map emitted as
	// `_neospec_rewritten_sources = {...}` before the hook. When set, the
	// coverage_hook's package.loaders / loadfile / dofile shims serve the
	// rewritten bytes so `_neospec_br(N)` counter calls fire at each arm
	// body. Enables exact per-arm BRDA in wrapped-runner reports instead
	// of the line-hit-derived approximation PopulateBranches produces.
	RewrittenSources map[string]string
}

// BuildShim constructs the Lua entry-point file for a cover-mode invocation.
// For plenary-busted and mini-test modes it embeds the coverage hook, the
// reporter, an output-capture wrapper, an on-exit autocmd, and the runner
// invocation into a single self-contained Lua script that Neovim runs via
// `-l`. For external mode BuildShim returns an error — external mode does
// not use a shim; callers set env vars and delegate hook loading to the user.
//
// Returns an error for invalid modes, empty output paths, missing per-mode
// fields, and paths containing NUL bytes (LuaJIT would silently truncate).
func BuildShim(opts ShimOpts) ([]byte, error) {
	if opts.OutputFile == "" {
		return nil, fmt.Errorf("cover: output file must not be empty")
	}
	if strings.ContainsRune(opts.OutputFile, 0) {
		return nil, fmt.Errorf("cover: output file contains a NUL byte: %q", opts.OutputFile)
	}

	switch opts.Mode {
	case RunnerPlenaryBusted, RunnerMiniTest:
		if opts.Dir == "" {
			return nil, fmt.Errorf("cover: %s mode requires --dir", opts.Mode)
		}
		if strings.ContainsRune(opts.Dir, 0) {
			return nil, fmt.Errorf("cover: dir contains a NUL byte: %q", opts.Dir)
		}
	case RunnerExternal:
		return nil, fmt.Errorf("cover: external mode does not use a shim")
	default:
		return nil, fmt.Errorf("cover: unknown runner mode %q", opts.Mode)
	}

	hook, err := runner.CoverageHookSource()
	if err != nil {
		return nil, fmt.Errorf("reading coverage hook: %w", err)
	}
	reporter, err := runner.ReporterSource()
	if err != nil {
		return nil, fmt.Errorf("reading reporter: %w", err)
	}

	var sb strings.Builder
	sb.Grow(len(hook) + len(reporter) + 2048)

	// Emit the coverage-include substring filter before the hook loads
	// so is_project_source consults it at every line event. Without
	// this, cover mode records every file the wrapped runner touched
	// including Neovim's own runtime — noise the run command's
	// filter already excludes.
	writeCoverageInclude(&sb, opts.CoverageInclude)

	// Emit the AST-recovered function-name map before the hook loads
	// so record_functions (called during _neospec_coverage_finalize)
	// sees real names for every function defined in a coverage_source
	// file. Anything not in the map — anonymous callbacks, or files
	// outside the source list — renders as "anonymous@N" per the
	// design decided in #43 when the NAME_PATTERNS fallback was retired.
	writeFunctionNames(&sb, opts.FunctionNames)

	// Emit the rewritten-source map before the hook loads so
	// coverage_hook's package.loaders / loadfile / dofile shims see
	// the global when they install themselves. Any require / dofile /
	// loadfile of a path in the map serves the rewritten bytes;
	// unmapped paths pass through to Neovim's default loader
	// unchanged. Enables exact per-arm BRDA in cover-mode reports
	// (same feature that landed in run mode via #38).
	writeRewrittenSources(&sb, opts.RewrittenSources)

	// Emit the resolved coverage-source list before the hook loads so
	// the finalizer sees it during _neospec_coverage_finalize and adds
	// zero-count entries for files no wrapped test touched. Without
	// this, cover-mode reports silently omit those modules — the exact
	// misleading-percentage failure mode the coverage_source knob was
	// designed to prevent, now available in cover mode too.
	writeCoverageSources(&sb, opts.CoverageSources)

	sb.Write(hook)
	sb.WriteByte('\n')
	sb.Write(reporter)
	sb.WriteByte('\n')
	sb.WriteString(fileCaptureWrapper(opts.OutputFile))
	sb.WriteByte('\n')
	sb.WriteString(exitAutocmd)
	sb.WriteByte('\n')
	sb.WriteString(runnerInvocation(opts.Mode, opts.Dir))
	sb.WriteByte('\n')
	sb.WriteString(`vim.cmd("qa!")` + "\n")

	return []byte(sb.String()), nil
}

// writeRewrittenSources emits `_neospec_rewritten_sources = { [path] =
// [==[ ... ]==], ... }` when the map is non-empty. Values are embedded
// as long-bracket strings with the smallest level that doesn't collide
// with any `]=*]` sequence inside the source — safer than double-
// quoted escaping for large Lua bodies that may contain arbitrary
// control chars inside string literals.
//
// Duplicated from runner/shim.go rather than exported: this concern is
// per-shim (cover mode and run mode each emit their own preamble), and
// the runner-side helper is unexported for the same reason. Following
// the existing convention set by writeCoverageSources / writeCoverageInclude /
// writeFunctionNames.
//
// Empty and nil inputs emit nothing so the coverage hook's loader
// shims never install themselves, and every file loads through
// Neovim's default loader unchanged.
func writeRewrittenSources(sb *strings.Builder, m map[string]string) {
	if len(m) == 0 {
		return
	}
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	sb.WriteString("_neospec_rewritten_sources = {\n")
	for _, path := range paths {
		src := m[path]
		level := longBracketLevel(src)
		eq := strings.Repeat("=", level)
		// Newline right after the opening bracket is eaten by Lua's
		// long-bracket rules; add one so any leading whitespace in
		// src is preserved.
		fmt.Fprintf(sb, "  [%q] = [%s[\n%s]%s],\n", path, eq, src, eq)
	}
	sb.WriteString("}\n")
}

// longBracketLevel returns the smallest N such that `]` followed by N
// `=` signs followed by `]` does not appear in src. That marker (level
// N) is then safe to use as the closing delimiter of a Lua long-bracket
// string containing src.
//
// Bounded at 32 defensively — Lua source cannot contain arbitrarily
// long `]=*]` runs syntactically outside string literals, but a
// pathological input should not spin.
func longBracketLevel(src string) int {
	for level := 0; level < 32; level++ {
		marker := "]" + strings.Repeat("=", level) + "]"
		if !strings.Contains(src, marker) {
			return level
		}
	}
	return 32
}

// writeFunctionNames emits `_neospec_function_names = { [path] = { [line]
// = "name", ... }, ... }` when the map is non-empty. Paths and line
// numbers are sorted so identical inputs produce identical shim bytes
// — same determinism promise every other cover-mode shim emission holds.
//
// Duplicated from runner/shim.go rather than exported: this concern is
// per-shim (cover mode and run mode each emit their own preamble), and
// the runner-side helper is unexported for the same reason. Following
// the existing convention set by writeCoverageSources / writeCoverageInclude.
//
// Empty and nil inputs emit nothing so the coverage hook's fallback
// (label every function "anonymous@N") runs unchanged.
func writeFunctionNames(sb *strings.Builder, m map[string]map[int]string) {
	if len(m) == 0 {
		return
	}
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	sb.WriteString("_neospec_function_names = {\n")
	for _, path := range paths {
		fmt.Fprintf(sb, `  ["%s"] = {`, luaEscape(path))
		lines := make([]int, 0, len(m[path]))
		for ln := range m[path] {
			lines = append(lines, ln)
		}
		sort.Ints(lines)
		for i, ln := range lines {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, `[%d]="%s"`, ln, luaEscape(m[path][ln]))
		}
		sb.WriteString("},\n")
	}
	sb.WriteString("}\n")
}

// writeCoverageInclude emits the `_neospec_coverage_include = {...}`
// Lua global that the coverage hook consults from is_project_source at
// every line event. Substrings are emitted in the user's given order
// (no sort) because the hook short-circuits on the first match — reorder
// changes the fast path but never the result set. luaEscape'd for path
// substrings that legitimately contain special chars.
//
// Empty and nil input emit nothing so the coverage hook's default
// (record every file) runs unchanged.
func writeCoverageInclude(sb *strings.Builder, patterns []string) {
	if len(patterns) == 0 {
		return
	}
	sb.WriteString("_neospec_coverage_include = {")
	for i, p := range patterns {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, `"%s"`, luaEscape(p))
	}
	sb.WriteString("}\n")
}

// writeCoverageSources emits the `_neospec_coverage_sources = {...}`
// Lua global with each path double-quoted and escaped via luaEscape.
// Sorted for deterministic shim bytes so callers hashing the shim to
// detect drift see identical output for identical inputs — the same
// determinism promise every other shim emission holds.
//
// Empty and nil input emit nothing so the coverage hook's default
// behaviour (record only files a test loaded) runs unchanged.
func writeCoverageSources(sb *strings.Builder, paths []string) {
	if len(paths) == 0 {
		return
	}
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Strings(sorted)
	sb.WriteString("_neospec_coverage_sources = {")
	for i, path := range sorted {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, `"%s"`, luaEscape(path))
	}
	sb.WriteString("}\n")
}

// fileCaptureWrapper rewrites _neospec_report to redirect its io.write output
// into the caller-supplied file. This is what prevents the reporter's JSON
// from being contaminated by the wrapped runner's own stdout writes (plenary
// prints test progress; that noise would break json.Unmarshal on stdout).
func fileCaptureWrapper(outputFile string) string {
	return fmt.Sprintf(`
local _neospec_original_report = _neospec_report
_neospec_report = function()
  local orig_write = io.write
  local buf = {}
  io.write = function(...)
    for i = 1, select("#", ...) do
      table.insert(buf, tostring(select(i, ...)))
    end
  end
  local ok, err = pcall(_neospec_original_report)
  io.write = orig_write
  if not ok then
    io.stderr:write("neospec cover: reporter failed: " .. tostring(err) .. "\n")
    return
  end
  local f, ferr = io.open("%s", "w")
  if not f then
    io.stderr:write("neospec cover: cannot open output file: " .. tostring(ferr) .. "\n")
    return
  end
  f:write(table.concat(buf))
  f:close()
end
`, luaEscape(outputFile))
}

// exitAutocmd wires the reporter to fire on VimLeavePre so it always runs
// regardless of how the wrapped runner terminates Neovim (successful qa!,
// test-failure cq, or a lua error causing an early exit).
const exitAutocmd = `
local _neospec_fired = false
vim.api.nvim_create_autocmd("VimLeavePre", {
  callback = function()
    if _neospec_fired then return end
    _neospec_fired = true
    _neospec_report()
  end,
})
`

// runnerInvocation returns the Lua fragment that invokes the wrapped runner
// programmatically for the given mode.
func runnerInvocation(mode RunnerMode, dir string) string {
	esc := luaEscape(dir)
	switch mode {
	case RunnerPlenaryBusted:
		// In-process invocation via plenary.busted.run(spec) per glob-
		// discovered file. Both PlenaryBustedFile and PlenaryBustedDirectory
		// delegate to plenary.test_harness which spawns a fresh nvim child
		// per spec (via Job:new; see test_harness.lua's test_paths). The
		// coverage hook installed in this parent process does not survive
		// into those children, so cover mode would serialize an empty map
		// and silently report 0%. plenary.busted.run is the raw entry
		// point the children ultimately call — invoking it directly here
		// keeps the specs in the current Lua state where the hook that
		// debug.sethook installed above sees every executed line.
		//
		// Discovery is a plain vim.fn.glob under Dir — matches plenary's
		// own **/*_spec.lua convention. pcall around each spec keeps a
		// single failing file from aborting the whole run; errors are
		// surfaced on stderr so the user still sees them.
		return fmt.Sprintf(`
local ok, busted = pcall(require, "plenary.busted")
if not ok then
  io.stderr:write("neospec cover: plenary.busted not found on runtimepath\n")
  vim.cmd("cq")
end
local specs = vim.fn.glob("%s/**/*_spec.lua", false, true)
for _, spec in ipairs(specs) do
  local sok, serr = pcall(busted.run, spec)
  if not sok then
    io.stderr:write("neospec cover: " .. spec .. ": " .. tostring(serr) .. "\n")
  end
end
`, esc)
	case RunnerMiniTest:
		return fmt.Sprintf(`
local ok, minitest = pcall(require, "mini.test")
if not ok then
  io.stderr:write("neospec cover: mini.test not found on runtimepath\n")
  vim.cmd("cq")
end
minitest.run({ collect = { find_files = function()
  return vim.split(vim.fn.glob("%s"), "\n", { trimempty = true })
end } })
`, esc)
	default:
		// BuildShim rejects unknown modes upstream; this branch is unreachable
		// but returned as a diagnostic in case a future refactor drops the
		// upstream guard.
		return fmt.Sprintf(`error("neospec cover: unreachable runner mode %q")`+"\n", mode)
	}
}

// luaEscape mirrors runner.luaEscape (which is unexported); duplicated here
// to keep the cover package self-contained rather than promoting an internal
// helper to the runner package's public surface. Handles the character set
// most likely to appear in file paths and to produce syntactically-broken
// Lua if left unescaped.
func luaEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
