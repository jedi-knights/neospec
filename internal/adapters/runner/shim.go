package runner

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

// writeFunctionNames emits `_neospec_function_names = { [path] = { [line]
// = "name", ... }, ... }` when the map is non-empty. Paths and line
// numbers are sorted so identical inputs always produce identical shim
// bytes — reproducibility of the sandboxed run is a hard constraint,
// and callers hash the shim to detect drift.
//
// Empty and nil inputs emit nothing so the coverage hook's fallback
// (NAME_PATTERNS) still runs. A `_neospec_function_names = {}` global
// would work but is noise.
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

// luaEscape escapes a string for safe embedding in a Lua double-quoted string
// literal. It handles backslash, double-quote, newline, carriage return, and
// tab — the characters most likely to appear in file paths and to produce
// syntactically broken Lua if left unescaped. NUL bytes are not handled here;
// callers must reject paths containing NUL before calling luaEscape (see
// buildShim).
func luaEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

//go:embed lua/*.lua
var luaFS embed.FS

// CoverageHookSource returns the embedded Lua source of the coverage hook,
// exposed so companion adapters (like the exec-matrix / coverage-only wrapper)
// can build their own shim without duplicating the hook. The returned bytes
// are safe to embed directly into a Lua string context; no further
// escaping is needed.
func CoverageHookSource() ([]byte, error) {
	return luaFS.ReadFile("lua/coverage_hook.lua")
}

// ReporterSource returns the embedded Lua source of the JSON reporter. It
// reads the _neospec_results and _neospec_coverage globals and writes a
// single JSON document to stdout via io.write; callers that need the output
// on a channel other than stdout should intercept io.write themselves.
func ReporterSource() ([]byte, error) {
	return luaFS.ReadFile("lua/reporter.lua")
}

// buildShim constructs the Lua entry-point that is written into the sandbox
// before each Neovim invocation. It concatenates the coverage hook and the
// test harness, then appends the dofile() call for the actual test file.
//
// When initFile is non-empty, a dofile() call for it is prepended before the
// coverage hook so that the init file runs before instrumentation starts and
// is not itself included in coverage data.
//
// When coverageSources is non-empty, a _neospec_coverage_sources global is
// emitted alongside it, listing resolved paths the hook should record at zero
// coverage if no test loads them.
//
// When coverageInclude is non-empty, a _neospec_coverage_include global is
// emitted before the coverage hook. The hook reads this global and skips any
// source file whose absolute path does not contain at least one of the listed
// substrings, restricting coverage to the plugin's own source tree.
//
// buildShim returns an error if either path contains a NUL byte. LuaJIT (used
// by Neovim) truncates double-quoted strings at NUL, producing a silent
// "file not found" rather than a clear diagnostic.
func buildShim(testFile, initFile string, coverageInclude, coverageSources []string, functionNames map[string]map[int]string) ([]byte, error) {
	if testFile == "" {
		return nil, fmt.Errorf("test file path must not be empty")
	}
	if strings.ContainsRune(testFile, 0) {
		return nil, fmt.Errorf("test file path contains a NUL byte: %q", testFile)
	}
	if strings.ContainsRune(initFile, 0) {
		return nil, fmt.Errorf("init file path contains a NUL byte: %q", initFile)
	}
	// The three error branches below are structurally unreachable. The //go:embed
	// directive above causes a compile-time error if any of the named Lua files
	// are missing from the source tree, so by the time the binary runs the files
	// are guaranteed present in luaFS. embed.FS.ReadFile only fails for absent
	// paths; the error returns are kept for API correctness only.
	hook, err := luaFS.ReadFile("lua/coverage_hook.lua")
	if err != nil {
		return nil, fmt.Errorf("reading coverage_hook.lua: %w", err)
	}

	harness, err := luaFS.ReadFile("lua/harness.lua")
	if err != nil {
		return nil, fmt.Errorf("reading harness.lua: %w", err)
	}

	reporter, err := luaFS.ReadFile("lua/reporter.lua")
	if err != nil {
		return nil, fmt.Errorf("reading reporter.lua: %w", err)
	}

	// Escape the test file path for embedding in a Lua string literal.
	escaped := luaEscape(testFile)

	var sb strings.Builder
	// Use 2× raw path lengths as an upper bound for escaped output (luaEscape
	// at most doubles the length by escaping every character).
	sb.Grow(len(hook) + len(harness) + len(reporter) + 2*len(initFile) + 2*len(testFile) + 128)

	if initFile != "" {
		// fmt.Fprintf on a strings.Builder always returns a nil error (the
		// builder's Write never fails). The return is intentionally ignored;
		// golangci-lint's errcheck exempts strings.Builder writes for this reason.
		fmt.Fprintf(&sb, `dofile("%s")`+"\n", luaEscape(initFile))
	}

	if len(coverageInclude) > 0 {
		sb.WriteString("_neospec_coverage_include = {")
		for i, pattern := range coverageInclude {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, `"%s"`, luaEscape(pattern))
		}
		sb.WriteString("}\n")
	}

	// Resolved source paths for files that may never be loaded by any test.
	// The hook adds them with all-zero counts so untouched modules count
	// against the total instead of vanishing from the report.
	if len(coverageSources) > 0 {
		sb.WriteString("_neospec_coverage_sources = {")
		for i, path := range coverageSources {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, `"%s"`, luaEscape(path))
		}
		sb.WriteString("}\n")
	}

	// AST-recovered function-name map: {path -> {line -> name}}. The
	// coverage hook consults this before falling back to its NAME_PATTERNS
	// regexes, so a name recovered from the AST (which handles multi-line
	// signatures, table-literal method fields, and other shapes the
	// patterns miss) always wins over a source-line pattern match.
	// Emitted before the hook so the global is set when record_functions
	// runs.
	writeFunctionNames(&sb, functionNames)

	sb.Write(hook)
	sb.WriteByte('\n')
	sb.Write(harness)
	sb.WriteByte('\n')
	sb.Write(reporter)
	sb.WriteByte('\n')
	fmt.Fprintf(&sb, `dofile("%s")`+"\n", escaped)
	sb.WriteString("_neospec_report()\n")

	return []byte(sb.String()), nil
}
