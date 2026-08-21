package cover

import "os"

// RewriteAll runs Rewrite over every file in paths, returning:
//   - sources: map from original file path → rewritten source
//   - injections: concatenated injection metadata across all files, with
//     unique BranchIDs across the whole run so a single ApplyBranchCounters
//     call can attribute counters back
//
// Best-effort: files that cannot be read are silently skipped (matches
// PopulateBranches / FunctionNameMap). Files with no branches produce no
// entry in sources (nothing to rewrite) and no injections.
//
// Returns nil, nil for an empty input so callers can distinguish "no
// source list configured" from "source list configured but no branches
// found" — the runner uses that distinction to decide whether to emit
// the _neospec_rewritten_sources Lua global at all.
func RewriteAll(paths []string) (map[string]string, []Injection) {
	if len(paths) == 0 {
		return nil, nil
	}
	// Single shared counter across files so BranchIDs stay unique — the
	// attribution helper (ApplyBranchCounters) keys on ID, so any two
	// files whose IDs collided would silently steal each other's counts.
	counter := 0
	nextID := func() int { counter++; return counter }
	opts := RewriteOptions{NextID: nextID}

	sources := make(map[string]string, len(paths))
	var injections []Injection
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out, injs, _ := Rewrite(path, src, opts)
		if len(injs) == 0 {
			// No control flow in this file — nothing was spliced in, so
			// out == src. Skip to keep the payload small and let the
			// harness's default loader handle these files.
			continue
		}
		sources[path] = string(out)
		injections = append(injections, injs...)
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return sources, injections
}
