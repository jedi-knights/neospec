# Branch instrumentation — design pass

Draft. Not yet implemented. This document explores the design space for
closing the branch-coverage gap that source-derived line hits cannot
reach, and recommends a specific approach. Update it as decisions
crystallize; when a phase ships, mark it done inline and note what the
next phase depends on.

See also: `docs/adoption-strategy.md` (the "Branch coverage" paragraph
under "The bet" names the same gap and pointed here) and
`internal/adapters/cover/branches.go` (the current source-derived
detection primitive).

## The gap

The BRDA emission that shipped in PRs #30–#32 attributes per-arm hits by
looking up the arm's first-body line in the file's line-hit map. This is
honest but leaves three shapes as `unknown` (rendered as LCOV `-`,
Cobertura `0%`, Coveralls `0`):

1. **Same-line constructs.** `if x then A end` on one line has the
   decision and the arm-body body on the same source line. The line hit
   is one count per execution regardless of which arm was taken; we
   cannot distinguish "we evaluated the condition and took `A`" from "we
   evaluated the condition and didn't".

2. **Short-circuit operators.** `a and b`, `x or default`. The RHS is
   evaluated only when the LHS matches the short-circuit rule. The line
   hook sees the expression's line but has no way to report whether the
   RHS ran.

3. **Implicit fall-through arms.** `if x then A end` has no `else`
   block, so the "did not take" arm has no line to score. Same for the
   loop-exit case (`while cond do A end` — did we ever exit?).

For most codebases, shape 3 dominates by count, shape 1 by frequency
(one-liner idioms are common in Lua), and shape 2 by importance
(short-circuit `or` for defaulting is everywhere).

Closing these gaps needs information the runtime doesn't currently
produce. The two families of solutions that would work:

- **Source rewriting**: parse Lua source, inject counter calls at branch
  sites, execute the rewritten source, aggregate counter values.
- **Bytecode instrumentation**: modify LuaJIT bytecode after compile.

Bytecode instrumentation is LuaJIT-specific, invasive, and hard to
debug. Source rewriting is what every established branch-coverage tool
(Istanbul for JS, coverage.py's branch mode, JaCoCo for JVM) uses. We
have a parser; we should use it.

## Constraints (do not violate)

The rewriter has to hold five properties simultaneously. Any design
that drops one is disqualified.

1. **Compatibility invariant.** Rewritten source must produce
   observationally identical test behaviour: same test outcomes, same
   error messages, same side-effect ordering. A user cannot know from
   the test log whether instrumentation was on.

2. **Line-number preservation.** Injections may not introduce newlines.
   `debug.getinfo(...).currentline`, error stack traces, and the
   existing line-coverage hook all key on line numbers; any shift
   corrupts every downstream report.

3. **Source-path fidelity.** `debug.getinfo(...).source` (aka the chunk
   name) must resolve to the original user file, not a shadow copy or a
   generated buffer. Anything else breaks user-visible error messages
   and every editor's "jump to source" affordance.

4. **Swim-lane discipline.** Rewriter code lives in
   `internal/adapters/cover/`; it uses `go-lua-parser` for parsing and
   nothing else. Not a linter, not a formatter, not an author-facing
   tool. Per `CLAUDE.md`'s carve-out.

5. **Opt-in.** Instrumentation-driven rewriting is off by default. The
   current line-derived BRDA is honest and covers the common case;
   users who need per-arm precision explicitly enable it. This
   preserves the risk profile of shipping without instrumentation for
   users who never touch the flag.

## Recommended approach: string-splice rewriter, in-Neovim loader hook

Two-part system that consumes the AST we already produce and hands
rewritten source to Neovim through a `package.loaders` entry.

### Go side

1. **Discover.** Use the existing include-list (same one that scopes
   line coverage). Files outside the list are never rewritten.
2. **Parse.** For each file, call `lua.Parse` (already imported via
   `go-lua-parser`).
3. **Plan.** Walk the AST and produce an ordered list of `Injection`
   records:
   ```go
   type Injection struct {
       Offset int    // byte offset in the original source
       Text   string // exact bytes to splice in
       BranchID  int    // sequential ID for later attribution
       ArmIndex  int    // which arm this injection scores
   }
   ```
4. **Splice.** Sort injections by offset descending; splice each into
   the original source string. Descending order means earlier offsets
   don't shift as we insert. Output is one rewritten source string per
   file.
5. **Serialize.** Emit two artefacts:
   - `{ original_path -> rewritten_source }` map for the Neovim loader.
   - `{ branch_id -> {file, line, arm_index} }` map for post-run
     attribution.
6. **Attribute.** After the test run, read `_neospec_br_counts` from
   Neovim and fill `BranchArm.Taken` directly using the ID map,
   bypassing the current line-hit-derived `takenFor`.

### Lua side

1. **Bootstrap script.** Loaded before any test file. Defines:
   ```lua
   _neospec_br_counts = {}
   function _neospec_br(id)
       _neospec_br_counts[id] = (_neospec_br_counts[id] or 0) + 1
   end
   ```
2. **Loader shim.** Prepends `package.loaders` with a function that:
   - Resolves the module name to a candidate file path (same way the
     stock loader does).
   - Looks up the path in the rewritten-source map.
   - If found: `return load(rewritten, "@" .. original_path, "t")`. The
     `@` prefix on the chunk name is the Lua convention for "this came
     from a file", and using `original_path` preserves source-path
     fidelity (constraint 3).
   - If not found: return nil so the next loader tries.
3. **Reporter.** The existing reporter already streams
   `_neospec_coverage`; add `_neospec_br_counts` to the same stream.

Distribution of the rewritten source map: pass as a JSON file that the
bootstrap script loads on init. Size scales with source size, but real
projects are megabytes at most and this happens once per subprocess.

### Same-line injection example (Phase 1)

Original (`m.lua`):
```lua
1: if x then A() end
2: return M
```

Rewritten (line numbers preserved; injections stay on their source
line, no newlines added):
```lua
1: if x then _neospec_br(1); A() end
2: return M
```

`debug.getinfo` for `A()` still reports `m.lua:1`. Line hook still
fires once for line 1 per iteration. Branch counter `1` increments on
every taken branch — the count is exact regardless of same-line
colocation, which was the whole point.

### Short-circuit injection example (Phase 2)

Original:
```lua
1: local x = a and b()
```

Rewritten:
```lua
1: local x = a and (function() _neospec_br(2); return b() end)()
```

Costs one function creation and call per short-circuit evaluation.
Acceptable for coverage runs (already slower than normal runs);
prohibitive for hot paths in production, which is why this is
opt-in and never enabled outside a coverage run.

### Implicit-arm injection example (Phase 3)

For `if x then A end` (no else), inject an else clause:
```lua
if x then _neospec_br(1); A else _neospec_br(2) end
```

For `while cond do A end`, inject after the loop:
```lua
while cond do _neospec_br(3); A end _neospec_br(4)
```

Both changes preserve line count when the `else`/post-loop injection
stays on the same source line as the closing `end`.

## Phasing

Ordered by impact-per-risk. Each phase ships behind the same opt-in
flag; users get incremental precision as later phases land.

- **Phase 1 — same-line resolution.** Inject `_neospec_br(N);` as the
  first statement of every arm body (if/elseif/else/while/repeat/for).
  Solves same-line ambiguity and eliminates the "unknown" for
  reachable arms. Zero effect on `and`/`or` and on implicit
  fall-through arms.
- **Phase 2 — short-circuit resolution.** Wrap `and`/`or` RHS in IIFEs
  so the counter fires only on evaluation. Adds meaningful branch
  count for the most common Lua idiom (`x = t or default`).
- **Phase 3 — implicit-arm resolution.** Inject synthetic `else`
  clauses for if-without-else and post-loop counters for
  `while`/`for`/`repeat`. Closes the last "unknown" arm.

Ship each phase as its own PR with regression tests against a corpus
of realistic Lua (extend `testdata/` with harpoon, plenary, or
telescope excerpts — same corpus we use for parser acceptance in
`go-lua-parser`).

## Open decisions

Named for follow-up so they don't become invisible.

1. **Flag surface.** `neospec run --branch-instrumentation=on|off`,
   or a boolean in config (`[coverage] branch_instrumentation = true`),
   or both? Consistency with the existing coverage flags argues for a
   config option first, CLI flag second.
2. **Global name.** `_neospec_br` mirrors `_neospec_coverage` /
   `_neospec_results`. Should we prefix with an underscore-underscore
   sequence (`__neospec_br`) for stronger collision avoidance? The
   existing globals use single-underscore; consistency wins unless
   there's a real collision case.
3. **Include-list scoping.** Rewriter uses the coverage include list.
   Should it also respect a separate exclude list for branch-only
   opt-out (e.g., a critical hot path the user knows will slow down
   under IIFE wrapping)? Probably yes eventually; add if requested.
4. **Reporter behaviour when instrumentation is on.** Do we still
   emit the source-derived-only branches for arms that the rewriter
   did not touch (Phase 1 doesn't touch implicit arms), or do we drop
   them so BRF/BRH reflect only instrumented data? Recommendation:
   keep both; the mixed rendering is what the current per-format
   "unknown" convention was designed for.
5. **Distribution format.** JSON file for the rewritten-source map is
   the obvious choice, but for very large projects it adds a
   one-time load cost per subprocess. Alternative: embed a Lua table
   literal into the bootstrap script. Table literals load faster than
   JSON parsing but bloat the bootstrap file. Punt until we measure.
6. **Interaction with `PlenaryBustedDirectory` companion mode.** The
   companion mode (adoption move #2) wraps an external runner; can we
   still rewrite its source? The rewriter operates on files, so yes
   — but the loader hook must be installed before the runner starts.
   Verify with a smoke test when companion mode ships.

## Risks and mitigations

- **Semantic drift under IIFE wrapping.** Wrapping `and`/`or` RHS in a
  closure creates a new scope; user code that references upvalues via
  `debug.getinfo` or `getfenv`/`_ENV` could observe the change. Very
  rare in test code but not zero. Mitigation: document; ship Phase 2
  behind an additional opt-in that separates "conservative"
  (statement-level only) from "aggressive" (expression-level).
- **Collision with user globals.** A codebase defining
  `_neospec_br` breaks. Mitigation: pick a distinctive name; if we
  ever see a real collision, add a per-run randomized suffix.
- **Rewriter bugs corrupt tests.** A bad injection turns a passing
  test into a failure or vice versa. Mitigation: differential test
  in CI — for each fixture in the go-lua-parser corpus, run the file
  once original and once rewritten and assert identical outputs
  (`load(src)()` results, error messages). No rewriter change ships
  without the diff test passing.
- **Line-shift regressions.** A future change to the rewriter adds a
  newline by accident. Mitigation: unit test asserts
  `strings.Count(rewritten, "\n") == strings.Count(original, "\n")`
  for every file in the corpus.
- **`package.loaders` ordering issue.** Something else (plenary,
  a plugin under test) prepends its own loader before ours,
  bypassing rewriting. Mitigation: install our loader as late as
  possible during bootstrap, and re-install after
  `require("plenary")` if plenary is in scope.

## Success criteria

The rewriter is correct when:

1. Differential test passes on every fixture: rewritten source
   produces identical output to original source for every input the
   test suite exercises.
2. Line coverage numbers do not change under instrumentation
   (same lines report the same hit counts).
3. Stack traces from a deliberately-broken test point at the
   original file and line, not a shadow file or a shifted line.
4. `_neospec_br_counts` fully accounts for BRF: every arm has a
   count, no arm is `Taken == -1`.
5. Instrumented BRH equals derived-BRH for cases where the derived
   approach was already correct (arms with distinct first-line
   coverage). Where they disagree, the instrumented count is right
   by construction.

## What to build first

Ship Phase 1 as a scaffolded PR that carries:

- `internal/adapters/cover/rewrite.go` — the string-splicer and
  injection planner.
- `internal/adapters/runner/lua/instrument_bootstrap.lua` — the
  loader shim and `_neospec_br` global.
- Runner wiring: when the config opt-in is set, produce the
  rewritten-source JSON and pass the bootstrap script into the
  test-runner Neovim invocation.
- Attribution: extend `cover.PopulateBranches` to consult the ID map
  when instrumentation ran, falling back to line-derived taken
  otherwise.
- Differential test in `testdata/`.

Phases 2 and 3 follow as separate PRs once Phase 1 is stable.
