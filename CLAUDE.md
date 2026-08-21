# neospec — Project Instructions

## What this is

A standalone Go binary that runs Lua tests for Neovim plugins in CI without requiring a system Neovim install. Ships a `plenary.busted`-compatible BDD harness (`describe` / `it` / `before_each` / `after_each` / `pending` / `assert.*`), collects line-level Lua coverage via `debug.sethook`, and emits LCOV / Cobertura / JUnit / Coveralls / console reports. Distributed as brew tap, GoReleaser binaries, `go install`, Docker, and a GitHub Action (`jedi-knights/neospec@v0`).

## Design philosophy anchors

Two constraints govern every non-trivial change. Both are load-bearing — flag any diff that touches either.

### 1. Compatibility invariant (do not break)

**Any Lua test file that runs under `PlenaryBustedDirectory` today must run under `neospec run` tomorrow with no source changes.** This is the wedge that makes neospec adoptable at all. Specifically:

- The DSL (`describe`, `it`, `before_each`, `after_each`, `pending`) is frozen at plenary.busted parity. New authoring primitives go under a `neospec.*` namespace, not by extending the existing globals.
- New `assert.*` methods must not shadow busted names with different semantics.
- Discovery pattern defaults may extend but must continue to match `test/**/*_spec.lua`.
- The `assert.matches` Lua-pattern vs. luassert-PCRE gap is documented, not "fixed" in a way that breaks Lua-pattern users.

Any proposed change that would force a plenary-busted user to rewrite tests either gets rejected or gated behind an opt-in flag. This constraint is the single most important thing to preserve as neospec evolves.

### 2. Stay in the CI-plumbing swim lane

neospec's differentiation is CI concerns — standalone binary, version pinning, sandboxed XDG env, coverage, report formats, GitHub Action. It is not an authoring tool. Do not build LSP mocking, treesitter fixtures, floating-window snapshotting, or other in-runtime helpers — that is what plenary and mini.test do inside the Neovim runtime, and it's a losing fight to compete with them there.

**Carve-out for source-derived coverage.** Go-side static analysis of Lua source is inside the swim lane *when its output feeds a coverage report* (BRDA in LCOV, similarly for other formats). Branch-point detection is not an authoring feature; users do not write against it. The companion repo [`go-lua-parser`](https://github.com/jedi-knights/go-lua-parser) exists specifically for this, and its consumption lives in `internal/adapters/cover/`. Analysis that would help plugin *authors* (linting, formatting, LSP-adjacent tooling) remains out of scope.

## Adoption strategy

Full analysis in [`docs/adoption-strategy.md`](docs/adoption-strategy.md). Read it before proposing user-facing changes. Short version:

- **tpope audience:** unreachable, do not chase. Vimscript-first, zero-test-suite by design.
- **tjdevries audience:** reachable *only* via a coverage-companion mode that wraps `PlenaryBustedDirectory` rather than replacing it. Direct-competition framing is politically dead.
- **theprimeagen audience:** primary target. plenary.busted user, has manual Neovim-install CI boilerplate, no coverage badges anywhere. Migration is nearly free once compatibility is proven.

The four highest-leverage moves (ordered by impact):

1. Lead the README's "Why neospec" with the compatibility claim, not the pain.
2. Ship coverage-only companion mode: `neospec cover --runner=plenary-busted --dir=tests/`. Wrap, don't replace.
3. Publish one non-jedi-knights case study — pick a small community plugin, PR their CI to use neospec, link the diff on the README.
4. Match `PlenaryBustedDirectory` local-dev ergonomics — `neospec run` from repo root must be at least as fast and clean as `make test` today.

## New-feature ranking rubric

Score 0–3 on each axis; ship only if total ≥ 6. Full definitions in [`docs/adoption-strategy.md`](docs/adoption-strategy.md#ranking-rubric-for-new-feature-proposals).

- **Compatibility** — preserves the invariant?
- **Coverage differentiation** — strengthens the "coverage no one else does" axis?
- **CI ergonomics** — shortens the user's workflow YAML?
- **Adoption unlock** — removes a stated blocker for the tj / prime audience?

Features that only strengthen jedi-knights-suite ergonomics are not adoption features. Fine to ship, but don't count against the adoption bet.

## What NOT to build

- A DSL richer than busted's (`context`, `xit`, `focus`, `randomize` — every new primitive is compatibility surface that can drift).
- A bespoke "neospec-native" report format — LCOV/Cobertura/JUnit/Coveralls already cover every consumer.
- In-runtime editor helpers (LSP mocking, treesitter fixtures, snapshot testing) — off-mission.
- A Vimscript harness — the audience for Vimscript-plugin testing is empty.
- A Lua config file (`.neospec.lua`) — TOML + env + flags already work; do not add a fourth precedence tier.
- A plugin manager, spec registry, or "neospec ecosystem" — scope creep.
- Lua *authoring* tooling in `internal/adapters/cover/` — the `go-lua-parser` dep is bounded to coverage-report input. Linting, formatting, or LSP-style features on top of it are off-mission (they compete with stylua / selene / lua-language-server, which own that ground).

## Stack

- Go 1.x, hexagonal architecture: `internal/domain/` (pure types, stdlib only) ← `internal/ports/` (interfaces) ← `internal/adapters/` (I/O). Everything points inward toward domain.
- CLI: `cmd/neospec/{main.go, commands/{run,version,cache}.go}` using cobra.
- Lua harness ships as `internal/adapters/runner/lua/{harness,coverage_hook,reporter}.lua` embedded and loaded into the sandboxed Neovim.
- Lua source analysis (branch-point detection for BRDA) uses the companion repo [`go-lua-parser`](https://github.com/jedi-knights/go-lua-parser); consumption lives in `internal/adapters/cover/`.
- Release: GoReleaser + semantic-release + GitHub Action composite in `action.yml`.
- Lint: `golangci-lint` v2, pre-push hook in `.githooks/pre-push`.

## Hard constraints

- **Functions ≤ 40 lines, cyclomatic complexity ≤ 7.** Enforced by `.golangci.yml`.
- **No globals; all dependencies injected via constructors.** Consistent with the hexagonal layering.
- **Domain package imports nothing beyond stdlib.** If you find yourself adding an import to `internal/domain/`, move the code to `internal/adapters/` instead.
- **Coverage stays ≥ 90%** (currently ~93.4%). New code must ship tests.

## Commit discipline

- Follow global Conventional Commits (see `~/.claude/CLAUDE.md`).
- semantic-release drives version bumps from `feat` / `fix` / breaking changes; other types no-op.
- One PR = one `type(scope)` pair.
- The `docs/adoption-strategy.md` file is authoritative — update it in the same PR as any strategy-affecting change so future readers see the reasoning next to the code.
