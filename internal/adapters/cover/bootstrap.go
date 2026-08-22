package cover

import "os"

// FunctionNameMap reads each source path, walks its AST for function
// definitions, and returns a two-level map keyed by (path, line) → name.
// The runner emits the result as a `_neospec_function_names` Lua global
// so the coverage hook can look up definition names without pattern-
// matching source lines.
//
// This is the Go side of the go-lua-parser replacement for the old
// coverage_hook.lua NAME_PATTERNS regex approach — now the sole
// source of function names. Any (path, line) the extractor did not
// cover renders as "anonymous@N" in the report; users who want real
// names for a file must include it in coverage_source.
//
// Best-effort: files that cannot be read are silently skipped (matches
// PopulateBranches). Parse errors do not abort the map — whatever
// functions survived recovery are still included.
//
// Returns nil for an empty input so callers can distinguish "no source
// list configured" (nil) from "source list configured but no functions
// found" (empty map). The runner uses that distinction to decide whether
// to emit the global at all.
func FunctionNameMap(paths []string) map[string]map[int]string {
	if len(paths) == 0 {
		return nil
	}
	out := make(map[string]map[int]string, len(paths))
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fs, _ := ExtractFunctions(path, src)
		if len(fs) == 0 {
			continue
		}
		m := make(map[int]string, len(fs))
		for _, f := range fs {
			// Last-wins on line collision: two functions defined on the
			// same source line are indistinguishable at debug.getinfo
			// resolution anyway, so a single label per line matches
			// what the runtime hook can attribute.
			m[f.Line] = f.Name
		}
		out[path] = m
	}
	return out
}
