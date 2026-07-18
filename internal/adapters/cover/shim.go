// Package cover instruments an existing Neovim test runner with coverage
// collection, without replacing the runner. It is the adapter behind
// `neospec cover`, the companion mode that unlocks adoption for users of
// plenary-busted and mini.test who don't want to switch test frameworks.
package cover

import (
	"fmt"
	"strings"

	"github.com/jedi-knights/neospec/internal/adapters/runner"
)

// RunnerMode names one of the supported wrapped-runner shapes. Each mode
// determines how the shim invokes the underlying test framework after the
// coverage hook is installed.
type RunnerMode string

const (
	// RunnerPlenaryBusted wraps plenary.nvim's test_harness.test_directory.
	// The user's minimal_init file must bootstrap plenary onto the runtimepath.
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
		return fmt.Sprintf(`
local ok, harness = pcall(require, "plenary.test_harness")
if not ok then
  io.stderr:write("neospec cover: plenary.test_harness not found on runtimepath\n")
  vim.cmd("cq")
end
harness.test_directory("%s", { sequential = true, keep_going = true })
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
