# neospec adoption strategy

Detailed notes on how to evolve neospec toward broader adoption in the wider Neovim plugin ecosystem — specifically the audiences represented by tpope, tjdevries, and theprimeagen. This document is the source of truth for adoption-driven design decisions. Update it when the strategy changes; use it to sanity-check new features.

## The bet

The three features that differentiate neospec from `plenary.busted` / `mini.test` / `nvim-test` / `luaunit` are all **CI concerns, not authoring concerns**:

| Concern | neospec | plenary.busted | mini.test |
|---|---|---|---|
| Standalone binary (no Neovim install needed by the runner) | ✅ | ❌ | ❌ |
| Neovim version pinning per run | ✅ | ❌ | ❌ |
| Sandboxed XDG env per run | ✅ | ❌ | ❌ |
| Line-level Lua coverage | ✅ | ❌ | partial |
| Branch-level Lua coverage (BRDA/BRF/BRH, source-derived) | ✅ | ❌ | ❌ |
| LCOV / Cobertura / JUnit / Coveralls output | ✅ | ❌ | ❌ |
| First-class GitHub Action | ✅ | via boilerplate | via boilerplate |
| Ecosystem gravity | small | huge | growing |

The wedge is **compatibility**: neospec's harness (`describe` / `it` / `before_each` / `after_each` / `pending` / `assert.*`) implements the same DSL names as `plenary.busted`, so any file already written for busted will run under neospec with zero source changes. That compatibility is what makes migration risk-free — and it is the single most important invariant to preserve as neospec evolves.

**Branch coverage (BRDA)** shipped across all three reporters: LCOV emits `BRDA/BRF/BRH`, Cobertura emits per-line `branch="true"` + `condition-coverage` + `<condition/>` elements with `branch-rate` roll-ups, and Coveralls emits the flat `branches` quadruple array. All three read the same source-derived `BranchCoverage` produced by `internal/adapters/cover.PopulateBranches`, which locates branch points via the companion repo [`go-lua-parser`](https://github.com/jedi-knights/go-lua-parser) — published standalone (MIT, same license) so other Go tooling that reasons about Lua source can consume it. See "Companion repos" below for why the AST work stays inside the swim lane despite touching source analysis.

Per-arm hit counts come from the existing line hook — no runtime instrumentation. Two shapes cannot be honestly scored from line hits alone: **same-line constructs** (`if x then A end` on one line — the decision and body share a hit count, so per-arm attribution is guesswork) and **short-circuit operators** (`and`/`or` — RHS evaluation is invisible without extra hooks). Those arms are recorded as "unknown" (LCOV `-`, Cobertura `0%`, Coveralls `0`) rather than falsely claimed as hit. Aggregate treatment is uniform across formats: unknown excluded from the hit numerator, included in the total. Closing this gap needs source rewriting before execution — a swim-lane-crossing change that warrants its own design pass, not a reporter add-on.

## Compatibility invariant (do not break)

**Any Lua test file that runs under `PlenaryBustedDirectory` today must run under `neospec run` tomorrow with no source changes.** This is the load-bearing claim. Every feature addition must be checked against it:

- New `assert.*` methods must not shadow existing busted names with different semantics.
- The `describe` / `it` / `before_each` / `after_each` / `pending` DSL is frozen at busted parity — new authoring primitives go under a `neospec.*` namespace.
- Discovery defaults may extend (`test/**/*_spec.lua` today) but the current pattern must continue to match.
- `assert.matches` uses Lua patterns; busted supports PCRE via luassert. This is a known gap — document it in the README's "porting from busted" section rather than "fixing" it in a way that breaks Lua-pattern users.

If a proposed change would force a plenary-busted user to rewrite tests, either reject the change or gate it behind an opt-in flag.

## Per-author positioning

### tpope — do not chase

- Zero `.github/workflows/`, zero tests, zero linters in any of his 40+ plugins.
- Correctness model is "my daily editor + 20k downstream users; bugs surface in days."
- Vimscript-first; Lua test frameworks are structurally off-mission.
- **Verdict:** neospec has nothing to offer him. Do not build features aimed at his workflow. His absence is not a bug in the tool.

### tjdevries — soft target via coverage, not runner replacement

- Co-author of `plenary.busted`. Telescope, plenary, colorbuddy all use `PlenaryBustedDirectory` in their Makefiles. Directly asking him to switch runners is a political dead end.
- **Genuine gap in his stack:** plenary busted has no coverage story. Telescope has no coverage badge. Colorbuddy has no CI at all. This is where neospec has honest utility for him.
- **Positioning that could land:** a *coverage-only companion mode* that instruments a `PlenaryBustedDirectory` run and emits reports without replacing the runner. Something like:
  ```
  neospec cover --runner=plenary-busted --dir=tests/
  ```
  This treats plenary as first-class, not competition.
- **What would kill the sale:** any framing that positions neospec as replacing plenary. His design philosophy ("build the missing utility once, put it in plenary, depend from everywhere") argues against depending on a competing test library. Coverage-as-companion sidesteps that objection.
- **Verdict:** unlikely to migrate flagship plugins. Reachable for coverage-only integration if the mode exists.

### theprimeagen — the strongest case; primary target

Prime uses `plenary.busted` for tests in harpoon, git-worktree, vim-apm. His CI does the exact dance neospec collapses:

```yaml
# what he writes today (harpoon/.github/workflows/ci.yml)
- name: install neovim
  run: |
    curl -L https://github.com/neovim/neovim/releases/download/... | tar -xz
    echo "$PWD/nvim-linux/bin" >> $GITHUB_PATH
- name: install plenary
  run: |
    git clone --depth 1 https://github.com/nvim-lua/plenary.nvim \
      ~/.local/share/nvim/site/pack/vendor/start/plenary.nvim
- name: test
  run: make test
```

Against what neospec collapses that to:

```yaml
- uses: jedi-knights/neospec@v0
```

Wins:
1. **Free coverage.** None of his plugins report coverage today; harpoon has 9.1k stars and no badge.
2. **Multi-version matrix without effort.** `neovim-version: nightly` on a second job costs one line.
3. **Consistent test setup across repos.** Every one of his plugins reinvents the same CI shape.

Blockers:
- refactoring.nvim already migrated *off* plenary onto `mini.test`. Runner-switching friction is real per-repo.
- His `Makefile` invokes `PlenaryBustedDirectory` directly for local dev. Neospec's local dev story must match — running `neospec run` locally must be as fast and ergonomic as `make test` today.

**Adoption path:** land on a small plugin first (git-worktree is ideal — small, uncovered, straightforward CI). If it lands cleanly, harpoon2 is the natural follow-up. Prime is willing to iterate on tooling (harpoon v1 → v2 is a rewrite); he is not sentimentally attached to plenary the way TJ is.

## The four moves that raise the adoption ceiling

Ordered by expected impact. Each is a concrete addition to neospec, not a marketing tactic.

### 1. Lead the README with compatibility

Right now the README opens with "why neospec" (the pain). Rewrite so the *first* claim under "Why" is:

> **Runs your existing plenary.busted tests unchanged.** neospec's harness implements the same `describe`/`it`/`before_each`/`after_each`/`assert.*` DSL. Point it at your existing `tests/**/*_spec.lua` files and it just works — no rewrites, no migration.

Compatibility-first framing converts "should I risk switching?" into "why not try it?" Add a short "porting from plenary.busted" section that lists the two-or-three semantic gaps (`assert.matches` pattern flavor, PCRE assertions, any `luassert`-specific methods not implemented) with the workaround for each. Honest gap disclosure is a trust signal.

### 2. Ship coverage-only companion mode

The TJ-audience unlock. Design goal:

```
neospec cover [--runner=plenary-busted|mini-test|external] \
              [--dir=tests/] \
              [--format=lcov,cobertura,junit,console] \
              [--threshold=80]
```

Semantics: instrument Lua via `debug.sethook` before the chosen runner starts; collect and emit reports after the runner exits; passthrough the runner's exit code. Do not replace the runner — wrap it.

This makes neospec valuable to every existing plenary-busted user without asking them to change their DSL, their `Makefile`, or their trust model. It also gives plugins that use `mini.test` (refactoring.nvim) or in-house harnesses a coverage path they otherwise lack.

If this mode exists, "neospec coverage" becomes an ecosystem-neutral utility rather than a competing runner. That framing is much easier to land in a wide audience.

### 3. Publish one non-jedi-knights case study

Adoption follows evidence. Right now every plugin using neospec is in the jedi-knights org. The credibility ceiling is set by whichever non-org plugin adopts it first — and there is currently no such plugin cited on the README.

Concrete plan:
- Pick one small, currently uncovered community plugin (~200-1000 stars, plenary-busted-based).
- Offer a PR that swaps their CI to `uses: jedi-knights/neospec@v0` with the same tests and adds a coverage badge.
- Link the resulting CI diff prominently on neospec's README ("case study: `<plugin>` reduced its CI to N lines and gained a coverage badge").

One good case study is worth more than any amount of feature work for adoption.

### 4. Make local dev match `PlenaryBustedDirectory` ergonomics

The single-command experience is critical for Prime-style adoption. `neospec run` from the repo root must:

- Discover `tests/**/*_spec.lua` by default (matches plenary convention).
- Complete in comparable time to `PlenaryBustedDirectory` on the same tests (currently a wall-clock question — measure it).
- Print failures with file:line click-to-jump paths that terminal emulators recognize.
- Cache the Neovim download so repeated local runs feel instant (already done — verify no cold-start regression).

If a user's first local `neospec run` is slower or noisier than their existing `make test`, adoption stops before it starts, regardless of CI benefit.

## What NOT to build

Design temptations that would waste effort or actively hurt adoption:

- **A DSL richer than busted's.** Tempting to add `context`, `xit`, `focus`, `randomize`. Every new DSL primitive is another thing a user has to learn and a compatibility surface that can drift. Freeze the DSL at busted parity; extend via `neospec.*` for anything genuinely new.
- **A "neospec-native" reporter output format.** LCOV / Cobertura / JUnit / Coveralls already cover every mainstream coverage service. A bespoke format has no consumer.
- **Deep Neovim-runtime features** (LSP mocking, treesitter fixtures, floating-window snapshotting). This is what plenary and mini.test do inside their runtimes. Neospec's differentiation is CI plumbing, not editor mocking. Stay in the swim lane.
- **A Vimscript harness.** Vimscript plugins do not have tests as a norm and tpope is the archetype who never will. Zero return.
- **A configuration DSL richer than TOML + env + flags.** Precedence is already documented and works. Do not add a Lua-config file or an in-repo `.neospec.lua`.
- **A plugin manager, spec registry, or "neospec ecosystem."** Scope creep. Every hour spent on ecosystem infra is an hour not spent on the compatibility + coverage + case-study path.
- **Lua authoring tooling in `internal/adapters/cover/`.** The `go-lua-parser` dependency is bounded to coverage-report input (BRDA + related). Growing it into a linter, formatter, or LSP layer competes with stylua / selene / lua-language-server, all of which own that ground and outclass anything neospec would ship as a side quest.

## Companion repos

- **[`go-lua-parser`](https://github.com/jedi-knights/go-lua-parser)** — Lua 5.1 lexer, parser, and AST for Go (with LuaJIT extensions accepted by default). MIT-licensed, published standalone. Extracted from neospec's swim lane on three properties:
  1. **Not in-runtime.** Runs in the Go pipeline, not inside a Neovim instance. The swim-lane rule targets in-runtime helpers.
  2. **Not authoring-adjacent.** Plugin authors never write against it; only tooling consumes it.
  3. **Plausibly reusable.** Other Go tools that reason about Lua source (linters, formatters, static analyzers written in Go) can depend on it without pulling in neospec.

  Neospec consumes it in `internal/adapters/cover/branches.go` for BRDA branch-point detection. If a future feature has the same three properties, it belongs in a similar companion repo rather than bloating neospec.

  Pinned version lives in `go.mod`. The parser is pre-1.0 so its API may shift between minor versions; upgrades happen deliberately, not automatically.

## Ranking rubric for new-feature proposals

Score a proposed feature 0–3 on each axis; ship only if the total is ≥ 6:

| Axis | 0 | 1 | 2 | 3 |
|---|---|---|---|---|
| **Compatibility** — does it preserve the invariant? | Breaks busted parity | Adds opt-in gap | No effect | Widens the wedge |
| **Coverage differentiation** — does it strengthen the "coverage no one else does" axis? | No | Slightly | Clearly | Uniquely |
| **CI ergonomics** — does it shorten the user's `.github/workflows/*.yml`? | Lengthens | No effect | Shortens | Eliminates a whole job |
| **Adoption unlock** — does it remove a stated blocker for the tj / prime audience? | No | Marginal | Named-author blocker | Multi-author blocker |

Features that only strengthen "already-adopter" workflows (jedi-knights-suite ergonomics) are not adoption features. Ship them, but don't count them against the adoption bet.

## Meta: how to evolve this document

- When a new named author or community plugin comes into scope, add a section under "per-author positioning" and re-rank targets.
- When one of "the four moves" ships, mark it done inline and add a paragraph on measured outcome (did adoption move? did a coverage-only user appear?).
- When a feature is proposed, apply the rubric and record the score here. Rejected features stay recorded so the same idea doesn't get re-proposed.
- The compatibility invariant is the one section that should never be relaxed without explicit reasoning above the change. If it ever needs to move, the change must ship with a documented migration path for existing users.
