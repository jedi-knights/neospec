<div align="center">

# neospec

**One binary that runs your Neovim plugin's Lua tests in CI and tells you what's covered.**

[![CI](https://github.com/jedi-knights/neospec/actions/workflows/ci.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/ci.yml)
[![Release](https://github.com/jedi-knights/neospec/actions/workflows/release.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/release.yml)
[![GoReleaser](https://github.com/jedi-knights/neospec/actions/workflows/goreleaser.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/goreleaser.yml)
[![Badge](https://github.com/jedi-knights/neospec/actions/workflows/badge.yaml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/badge.yaml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Coverage](https://img.shields.io/badge/Coverage-88.3%25-green)](https://jedi-knights.github.io/neospec/?v=45)

</div>

---

## Table of contents

- [Overview](#overview)
- [Which mode do I want?](#which-mode-do-i-want)
- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Usage](#usage)
  - [`neospec run` — replace your test runner](#neospec-run--replace-your-test-runner)
  - [`neospec cover` — keep your test runner, add coverage](#neospec-cover--keep-your-test-runner-add-coverage)
  - [`neospec exec` — run any command across many Neovim versions](#neospec-exec--run-any-command-across-many-neovim-versions)
  - [`neospec cache` — manage the downloaded Neovim binaries](#neospec-cache--manage-the-downloaded-neovim-binaries)
- [Examples](#examples)
- [GitHub Action](#github-action)
- [Writing tests for `neospec run`](#writing-tests-for-neospec-run)
- [Porting from plenary.busted](#porting-from-plenarybusted)
- [Configuration](#configuration)
- [Neovim version management](#neovim-version-management)
- [Platform support](#platform-support)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

If you write Neovim plugins in Lua, your CI job probably looks like this today:

1. Download a Neovim tarball with `curl`.
2. Extract it into a directory.
3. Prepend that directory to `PATH`.
4. `git clone` [`plenary.nvim`](https://github.com/nvim-lua/plenary.nvim) into a specific spot on `runtimepath` so `PlenaryBustedDirectory` will work.
5. Write a `minimal_init.vim` that loads plenary before your tests.
6. Run `nvim --headless -c "PlenaryBustedDirectory tests/" -c "qa!"` and hope the exit code means what you think it means.
7. Ship without a coverage badge, because measuring Lua coverage inside `nvim --headless` was never in scope for any of the tools above.

Every Neovim plugin repo re-solves that same puzzle. It works, it's fragile, and it silently drifts every time upstream Neovim reorganizes a release asset (they renamed `nvim-linux64.tar.gz` to `nvim-linux-x86_64.tar.gz` at v0.10.4 — did your CI notice?).

**neospec is one binary that does the whole puzzle.** You either point it at your existing test files and use it as your test runner (`neospec run`), or you keep your existing runner and let neospec add coverage on top (`neospec cover`). Either way you get:

- A pinned, verified Neovim install — no `curl | tar` in your workflow.
- A clean, sandboxed `XDG` environment for every run so your tests cannot read or corrupt your real Neovim config.
- Line and branch coverage collected via Lua's `debug.sethook` (no source changes required from you).
- Reports in the formats your CI already understands: **LCOV, Cobertura XML, JUnit XML, Coveralls JSON, and a color console summary.**
- A GitHub Action wrapper so opting in is a single `uses:` line.

The tool is intentionally not a "Neovim testing ecosystem." It does not mock LSP, it does not fake treesitter, it does not snapshot floating windows — plenary and [mini.test](https://github.com/echasnovski/mini.test) already do those things well inside the Neovim runtime, and neospec has no interest in competing with them there. **neospec's job is the CI plumbing around your tests, not the tests themselves.**

## Which mode do I want?

If you already have tests, pick based on your current setup:

| Your situation | Use | Why |
|---|---|---|
| You wrote your tests with `describe` / `it` and run them under `PlenaryBustedDirectory` | Try `neospec run` first | The DSL is compatible — usually zero code changes and you get coverage for free |
| You use plenary.busted heavily (spies, stubs, `assert.same`) and don't want to change | `neospec cover --runner=plenary-busted` | Keeps your runner, just adds coverage reports |
| You use `mini.test` | `neospec cover --runner=mini-test` | Same idea — your runner stays, coverage appears |
| You have a `make test` target you don't want to touch | `neospec cover --runner=external -- make test` | Coverage instrumentation via env vars, you drive the command |
| You want to sanity-check your plugin against multiple Neovim versions | `neospec exec --versions=stable,nightly,v0.10.4 -- <cmd>` | Runs the same command once per version, aggregates the results |
| You have no tests yet and you're starting from scratch | `neospec run` with test files under `test/**/*_spec.lua` | Simplest possible surface — one binary, one command, no bootstrap files |

If you're not sure, start with `neospec run`. If it can't run your tests unchanged, fall back to `neospec cover`.

## Features

- **Standalone binary.** No system-level Neovim install required. neospec downloads and caches the Neovim version you ask for.
- **Version pinning.** `stable`, `nightly`, or a specific tag like `v0.10.4` — pinned per run so your CI is reproducible.
- **Sandboxed environment.** Every run gets a fresh `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, and `XDG_STATE_HOME` so tests cannot see your real Neovim config.
- **plenary.busted-compatible DSL.** `describe`, `it`, `before_each`, `after_each`, `pending`, and the common `assert.*` methods. Move your existing spec files over unchanged.
- **Line coverage** collected via `debug.sethook`. No source annotations, no build step.
- **Branch coverage** (`BRDA/BRF/BRH` in LCOV, `condition-coverage` in Cobertura, flat `branches` in Coveralls). Line-hit-derived by default; optionally exact via `--branch-instrumentation`, which rewrites sources ahead of time.
- **Function coverage** (`FN/FNDA/FNF/FNH` in LCOV) with real Lua function names recovered from the AST — not `anonymous@42`.
- **Report formats:** console, LCOV, Cobertura XML, JUnit XML, Coveralls JSON. Emit as many as you want in one run.
- **Companion mode** (`neospec cover`) wraps plenary.busted, mini.test, or your own command — adds coverage without replacing the runner.
- **Matrix mode** (`neospec exec`) runs any command across multiple Neovim versions.
- **GitHub Action** shipped in this repo — `uses: jedi-knights/neospec@v0` and you're done.

## Requirements

- **A supported OS.** Linux (x86_64 or arm64), macOS (Intel or Apple Silicon), or Windows (x86_64). See [Platform support](#platform-support).
- **~30 MB free in your cache directory** (default `~/.cache/neospec`) per Neovim version you use. The Neovim binary is downloaded once and reused.
- **Network access at first run** to download Neovim from the official [neovim/neovim releases](https://github.com/neovim/neovim/releases). Subsequent runs use the cache.
- **No pre-installed Neovim required.** neospec downloads its own.
- **For `neospec cover --runner=plenary-busted`:** [plenary.nvim](https://github.com/nvim-lua/plenary.nvim) must be reachable through the `--minimal-init` file you pass. neospec does not install plenary for you in cover mode.
- **For development on neospec itself:** Go 1.x, `golangci-lint` v2. See [Development](#development).

## Installation

### Homebrew (macOS and Linux)

```bash
brew install jedi-knights/tap/neospec
```

### Pre-built binaries

Download the latest release for your platform from the [Releases page](https://github.com/jedi-knights/neospec/releases), or use `curl`:

```bash
# Linux x86_64
curl -fsSL https://github.com/jedi-knights/neospec/releases/latest/download/neospec-linux-x86_64 \
  -o /usr/local/bin/neospec && chmod +x /usr/local/bin/neospec

# macOS (Apple Silicon)
curl -fsSL https://github.com/jedi-knights/neospec/releases/latest/download/neospec-darwin-arm64 \
  -o /usr/local/bin/neospec && chmod +x /usr/local/bin/neospec
```

### Go install

```bash
go install github.com/jedi-knights/neospec/cmd/neospec@latest
```

### Docker

```bash
docker pull ghcr.io/jedi-knights/neospec:latest
docker run --rm -v "$PWD":/workspace -w /workspace ghcr.io/jedi-knights/neospec run
```

### GitHub Action (no local install needed)

If your only use of neospec is in CI, skip local installation and jump to [GitHub Action](#github-action).

## Usage

### `neospec run` — replace your test runner

**Why you'd use it.** You want the simplest possible CI setup: one binary, one command. You either don't have tests yet, or your existing tests are written with the `describe` / `it` DSL that plenary.busted popularized (see [Porting from plenary.busted](#porting-from-plenarybusted) for the small number of differences that can trip a migration).

**What it does.**

1. Discovers Lua test files matching `test/**/*_spec.lua` (override with `--pattern`, repeatable).
2. Downloads and caches the Neovim version you asked for (default `stable`).
3. Spawns a headless Neovim in a sandboxed `XDG` environment.
4. Runs a `plenary.busted`-compatible harness inside Neovim — your test files load unchanged.
5. Records every executed Lua line via `debug.sethook`.
6. Writes reports in every format you asked for (`--format=console --format=lcov ...`).
7. Optionally fails the run when coverage drops below `--threshold`.

**How you use it.**

```bash
# The simplest possible invocation. Discovers test/**/*_spec.lua, prints a console summary.
neospec run

# CI-shaped: emit LCOV for the coverage service, fail if coverage < 80%.
neospec run --format=console --format=lcov --threshold=80

# Pin the Neovim version.
neospec run --neovim-version=v0.10.4

# Different test layout.
neospec run --pattern='spec/**/*_spec.lua'

# Restrict coverage to your plugin's own source (skip Neovim's runtime files).
neospec run --coverage-include=lua/

# Include a file in the coverage report even if no test loads it (avoids the
# "silent 100%" bug where untested modules disappear from the denominator).
neospec run --coverage-source='lua/**/*.lua'

# Bootstrap a runtime setup before tests load (equivalent to plenary's minimal_init.lua).
neospec run --init-file=tests/minimal_init.lua

# Turn on source-rewriting branch coverage (exact per-arm counts, opt-in).
neospec run --coverage-source='lua/**/*.lua' --branch-instrumentation
```

Sample output:

```
$ neospec run

  ✓ parser > handles empty input
  ✓ parser > tokenizes identifiers
  ✗ parser > rejects invalid syntax
    expected error, got nil

Tests: 2 passed, 1 failed  (0.31s)
Coverage: 73.4% (142/193 lines)
```

**All `neospec run` flags.**

| Flag | Default | Purpose |
|---|---|---|
| `-c, --config` | `neospec.toml` | Path to the TOML config file (missing file is fine — defaults apply). |
| `--neovim-version` | `stable` | `stable`, `nightly`, or a specific tag like `v0.10.4`. |
| `--pattern` (repeatable) | `test/**/*_spec.lua` | Glob(s) for discovering test files. |
| `--format` (repeatable) | `console` | Any of `console`, `lcov`, `cobertura`, `coveralls`, `junit`. |
| `--coverage-dir` | `coverage` | Where non-console report files are written. |
| `--threshold` | `0` (disabled) | Fail the run when coverage is below this percentage. |
| `--cache-dir` | `~/.cache/neospec` | Where downloaded Neovim binaries live. |
| `--init-file` | none | Path to a Lua file executed before the coverage hook (analogous to `minimal_init.lua`). Not itself instrumented. |
| `--coverage-include` (repeatable) | none | Only record files whose absolute path contains this substring. Use to exclude Neovim's runtime. |
| `--coverage-source` (repeatable) | none | Glob of files that must appear in the report even if untested. Prevents "silent 100%" on holes in the suite. |
| `--branch-instrumentation` | `false` | Rewrite Lua sources ahead of time so per-arm branch counts are exact. See [Configuration](#configuration) for cost. |
| `-v, --verbose` | `false` | Print extra diagnostic output to stderr. |

### `neospec cover` — keep your test runner, add coverage

**Why you'd use it.** You already have a test suite running under plenary.busted or mini.test, you don't want to migrate, and you want a coverage badge without re-inventing your CI job. This mode wraps the existing runner rather than replacing it — your test framework's semantics (spies, stubs, `assert.same`, custom matchers, ordering rules, everything) stay intact. Only the coverage instrumentation is added.

There is also an **external** submode that runs an arbitrary command of your choosing (typically `make test`) and gives your command the env vars it needs to load the coverage hook itself. Use this when your CI setup is more elaborate than "point at a directory."

**What it does.**

For `plenary-busted` and `mini-test` submodes:

1. Downloads the Neovim version you asked for.
2. Generates a Lua shim that installs the coverage hook and wires a `VimLeavePre` autocmd to serialize collected coverage to a file on exit.
3. Invokes the wrapped runner (`plenary.test_harness.test_directory` or `MiniTest.run`) from inside the shim, using **your** existing `--minimal-init` file to bootstrap the runtimepath.
4. Reads the serialized coverage back and emits reports in your chosen formats.

For `external` submode:

1. Downloads the Neovim version you asked for.
2. Writes the coverage hook Lua source to a temp file.
3. Sets `NEOSPEC_COVER_HOOK` and `NEOSPEC_COVER_OUTPUT` in your command's environment.
4. Runs your command (whatever comes after `--`).
5. Reads the serialized coverage back and emits reports.

Your command is responsible for loading the hook — typically `nvim -c "luafile $NEOSPEC_COVER_HOOK" ...` — and for ensuring the reporter fires before Neovim exits.

**Cover mode does not emit JUnit XML** because it has no test-results data to serialize into JUnit's schema (the wrapped runner owns test pass/fail parsing, not neospec). If you need JUnit output alongside coverage, use `neospec run` or emit JUnit from your existing runner's own reporter.

**How you use it.**

```bash
# plenary.busted with your existing minimal_init.vim.
neospec cover --runner=plenary-busted --dir=tests/ \
  --minimal-init=tests/minimal_init.vim \
  --format=console --format=lcov --threshold=80

# mini.test
neospec cover --runner=mini-test --dir=tests/ \
  --minimal-init=scripts/minimal_init.lua \
  --format=lcov

# External command — your Makefile drives Neovim; neospec only adds coverage.
neospec cover --runner=external --format=lcov -- make test

# Include files that no test loads (avoids the "silent 100%" bug).
neospec cover --runner=plenary-busted --dir=tests/ \
  --minimal-init=tests/minimal_init.vim \
  --coverage-source='lua/**/*.lua' --format=lcov

# Exact per-arm branch counts via source rewriting (opt-in).
neospec cover --runner=plenary-busted --dir=tests/ \
  --minimal-init=tests/minimal_init.vim \
  --coverage-source='lua/**/*.lua' --branch-instrumentation --format=lcov
```

**All `neospec cover` flags.**

| Flag | Default | Purpose |
|---|---|---|
| `-c, --config` | `neospec.toml` | Path to the TOML config file. |
| `--runner` | **required** | `plenary-busted`, `mini-test`, or `external`. |
| `--dir` | none | Test directory or glob. Required for `plenary-busted` and `mini-test`; ignored for `external`. |
| `--minimal-init` | none | Path to your existing init file (the same one plenary or mini.test needs). Used verbatim. |
| `--neovim-version` | `stable` | `stable`, `nightly`, or a specific tag. |
| `--format` (repeatable) | `console` | Any of `console`, `lcov`, `cobertura`, `coveralls`. **No `junit`** — see above. |
| `--coverage-dir` | `coverage` | Where non-console report files are written. |
| `--coverage-source` (repeatable) | none | Glob of files that must appear in the report even if untested. |
| `--coverage-include` (repeatable) | none | Only record files whose absolute path contains this substring. |
| `--branch-instrumentation` | `false` | Rewrite Lua sources for exact per-arm branch counts. |
| `--threshold` | `0` (disabled) | Fail when coverage is below this percentage. |
| `--cache-dir` | `~/.cache/neospec` | Where downloaded Neovim binaries live. |
| `-v, --verbose` | `false` | Extra diagnostic output. |

For `external` mode, everything after `--` is the command neospec runs, e.g. `neospec cover --runner=external -- make test`.

### `neospec exec` — run any command across many Neovim versions

**Why you'd use it.** You want to verify that your plugin still loads (or your tests still pass) on multiple Neovim versions — for example `stable` and `nightly` before you push a release. This is not a testing feature per se; it's a version-matrix wrapper for any command.

**What it does.**

1. Parses your comma-separated version list.
2. Downloads and caches each Neovim version.
3. For each version: prepends that version's `bin/` directory to `PATH`, applies the same sandboxed `XDG` environment `run` uses, and executes the command you supplied.
4. Aggregates the per-version results (exit code, duration, stdout, stderr).
5. Prints a per-version summary and a final tally; exits non-zero if any version failed.

**How you use it.** The `--` separates neospec's own flags from the command being wrapped:

```bash
# Sanity-check that both stable and nightly launch.
neospec exec --versions=stable,nightly -- nvim --version

# Run a plenary-busted suite against three versions.
neospec exec --versions=stable,nightly,v0.10.4 -- \
  nvim --headless -c "PlenaryBustedDirectory tests/" -c "qa!"

# Emit machine-readable JSON so CI can consume the matrix without parsing text.
neospec exec --versions=stable,nightly --format=json -- nvim --version
```

Sample JSON output:

```json
{
  "command": ["nvim", "--version"],
  "versions": [
    {"version": "stable", "passed": true, "exit_code": 0, "duration_ms": 320, "stdout": "NVIM v0.10.4\n", "stderr": ""},
    {"version": "nightly", "passed": true, "exit_code": 0, "duration_ms": 480, "stdout": "NVIM v0.11.0-dev\n", "stderr": ""}
  ],
  "summary": {"total": 2, "passed": 2, "failed": 0, "duration_ms": 800}
}
```

**All `neospec exec` flags.**

| Flag | Default | Purpose |
|---|---|---|
| `-c, --config` | `neospec.toml` | Path to the TOML config file. |
| `--versions` | **required** | Comma-separated versions (e.g. `stable,nightly,v0.10.4`). |
| `--format` | `console` | `console` or `json`. |
| `--cache-dir` | `~/.cache/neospec` | Where downloaded Neovim binaries live. |
| `-v, --verbose` | `false` | Extra diagnostic output. |

### `neospec cache` — manage the downloaded Neovim binaries

**Why you'd use it.** Downloaded Neovim binaries persist in the cache indefinitely. If you want to see what's on disk, or free up space, this is how.

**What it does.**

- `neospec cache list` — lists every cached version and the on-disk size of each.
- `neospec cache clean` — deletes the entire cache directory. The next `run` / `cover` / `exec` will re-download whatever it needs.

**How you use it.**

```bash
neospec cache list

VERSION               SIZE
-------               ----
stable                28.4 MB
v0.10.4               27.1 MB
```

```bash
neospec cache clean
Removed cache directory: /home/user/.cache/neospec
```

Neither subcommand takes flags.

## Examples

Below are three progressively more elaborate real-world setups.

### 1. The simplest possible CI: your plugin has tests, you want coverage

You already have `test/parser_spec.lua`. You want a green check on every PR and a coverage percentage you can display. In your repo:

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: jedi-knights/neospec@v0
        with:
          formats: console,lcov
          threshold: "80"
```

That's it. On every push you get a full test run against `stable` Neovim, a console summary, an LCOV file at `coverage/lcov.info` (which you can hand to Codecov / Coveralls), and a red build if coverage drops below 80%.

### 2. You use plenary.busted and don't want to switch runners

You have a `Makefile` that already calls `PlenaryBustedDirectory`. You like plenary's `assert.same`, spies, and stubs. You don't want to migrate. You just want a coverage report. In your CI:

```yaml
- uses: actions/checkout@v4
- uses: actions/setup-node@v4   # or however you install curl/tar for GH Actions
- name: Install neospec
  run: |
    curl -fsSL https://github.com/jedi-knights/neospec/releases/latest/download/neospec-linux-x86_64 \
      -o /usr/local/bin/neospec && chmod +x /usr/local/bin/neospec
- name: Install plenary
  run: git clone --depth 1 https://github.com/nvim-lua/plenary.nvim /tmp/plenary.nvim
- name: Run tests with coverage
  run: |
    neospec cover --runner=plenary-busted \
      --dir=tests/ \
      --minimal-init=tests/minimal_init.vim \
      --coverage-source='lua/**/*.lua' \
      --format=console --format=lcov \
      --threshold=80
```

Your existing `tests/minimal_init.vim` is used unchanged — it's still the file responsible for putting plenary on `runtimepath`. The only difference is that neospec is the process that invokes Neovim, and it wires the coverage hook in before your tests run.

### 3. You want to catch regressions against Neovim nightly before your users do

Two workflow jobs — one that gates PRs on `stable`, one that runs on a schedule against `stable` and `nightly` and opens an issue on failure:

```yaml
# .github/workflows/nightly.yml
name: Nightly matrix
on:
  schedule:
    - cron: "0 6 * * *"  # 6am UTC daily
  workflow_dispatch:
jobs:
  matrix:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install neospec
        run: |
          curl -fsSL https://github.com/jedi-knights/neospec/releases/latest/download/neospec-linux-x86_64 \
            -o /usr/local/bin/neospec && chmod +x /usr/local/bin/neospec
      - name: Matrix run
        run: |
          neospec exec --versions=stable,nightly --format=json -- \
            nvim --headless -c "PlenaryBustedDirectory tests/" -c "qa!" \
            | tee matrix.json
```

Parse `matrix.json` in a follow-up step to open an issue when `summary.failed > 0`.

## GitHub Action

The action wraps `neospec run`, so use it when that mode is what you want. For `cover` or `exec`, invoke the binary directly (Example 2 above shows how).

```yaml
- uses: jedi-knights/neospec@v0
  with:
    neovim-version: stable      # stable | nightly | v0.10.4
    formats: console,lcov       # comma-separated
    threshold: "80"             # fail if coverage < 80%
```

### Action inputs

| Input | Default | Description |
|---|---|---|
| `neovim-version` | `stable` | Neovim version — `stable`, `nightly`, or a tag like `v0.10.4`. |
| `test-patterns` | `test/**/*_spec.lua` | Comma-separated glob patterns for test discovery. |
| `coverage-dir` | `coverage` | Directory for coverage report files. |
| `formats` | `console` | Comma-separated output formats. |
| `threshold` | `0` | Minimum coverage percentage; `0` disables the gate. |
| `verbose` | `false` | Verbose output. |

### Action outputs

| Output | Description |
|---|---|
| `coverage-percentage` | Coverage percentage as a bare number (e.g. `87.5`). Empty if the console format was not enabled. |
| `passed` | `"true"` or `"false"` — whether the run passed all checks. |

## Writing tests for `neospec run`

neospec ships a `plenary.busted`-compatible BDD harness. Test files are discovered by glob pattern (default `test/**/*_spec.lua`).

```lua
-- test/parser_spec.lua
local parser = require("myplugin.parser")

describe("parser", function()
  local subject

  before_each(function()
    subject = parser.new()
  end)

  after_each(function()
    subject = nil
  end)

  describe("tokenize", function()
    it("handles empty input", function()
      assert.equals(0, #subject.tokenize(""))
    end)

    it("tokenizes identifiers", function()
      local tokens = subject.tokenize("foo bar")
      assert.equals(2, #tokens)
      assert.equals("foo", tokens[1].value)
    end)
  end)

  describe("parse", function()
    it("returns an AST node", function()
      local node = subject.parse("foo")
      assert.is_not_nil(node)
      assert.equals("identifier", node.type)
    end)

    it("rejects invalid syntax", function()
      assert.has_error(function()
        subject.parse("!!!invalid")
      end)
    end)

    pending("unicode support", function()
      -- not yet implemented
    end)
  end)
end)
```

### Assertion reference

| Assertion | What it does |
|---|---|
| `assert.equals(expected, actual [, msg])` | Strict equality (`==`). |
| `assert.not_equals(unexpected, actual [, msg])` | Strict inequality. |
| `assert.is_true(v [, msg])` | Value is exactly `true`. |
| `assert.is_false(v [, msg])` | Value is exactly `false`. |
| `assert.is_nil(v [, msg])` | Value is `nil`. |
| `assert.is_not_nil(v [, msg])` | Value is not `nil`. |
| `assert.has_error(fn [, msg])` | Calling `fn` raises an error. |
| `assert.matches(pattern, str [, msg])` | String matches a Lua pattern. |

Every assertion accepts an optional final `msg` argument that overrides the default failure message.

### Test lifecycle

```
describe block entered
  └─ before_each (all outer blocks, outermost first)
       └─ it block executed
  └─ after_each (all outer blocks, innermost first)
describe block exited
```

`pending` marks a test as skipped without running it. It appears in the console output and in JUnit XML as a skipped test.

## Porting from plenary.busted

Every file that runs under `PlenaryBustedDirectory` should run under `neospec run` unchanged — the DSL is deliberately API-compatible. A small number of narrow gaps exist where neospec's harness intentionally differs from [luassert](https://github.com/lunarmodules/luassert) (the assertion library plenary vendors). Each is called out below with its workaround.

### Assertion differences

| Method | plenary.busted / luassert | neospec |
|---|---|---|
| `assert.matches(pattern, str)` | Lua pattern by default; PCRE via `.re` | Lua pattern only. |
| `assert.same(expected, actual)` | Deep-equal via luassert | Not implemented — use `assert.equals` for scalars; walk structure explicitly for deep comparisons. |
| `assert.truthy(v)` / `assert.falsy(v)` | Loose truthiness | Use `assert.is_true` / `assert.is_false` for strict boolean checks. |
| `assert.spy(fn).was.called()` | Spy via luassert's `spy` module | Not implemented — inject test doubles at construction sites (DI). |
| `assert.stub(mod, "fn").returns(v)` | Stub via luassert's `stub` module | Not implemented — same reasoning as spies. |

If your suite uses `assert.same` or the spy/stub API extensively, prefer [`neospec cover`](#neospec-cover--keep-your-test-runner-add-coverage) over `neospec run` — cover keeps your existing plenary invocation and just adds coverage instrumentation on top.

### Discovery differences

- plenary.busted matches `*_spec.lua` under any directory you name at the `PlenaryBustedDirectory` call site; neospec defaults to `test/**/*_spec.lua` but accepts arbitrary glob patterns via `--pattern` (repeatable).
- `PlenaryBustedFile <file>` maps to `neospec run --pattern=<file>`.

### Bootstrapping differences

- plenary.busted requires a `minimal_init.vim` (or `.lua`) that puts plenary on the `runtimepath` before tests run; neospec's own harness is embedded in the binary so no runtimepath bootstrap is needed for `neospec run`. If you still need setup Lua (e.g. mocking a dependency), point `--init-file` at it.
- If you use `neospec cover --runner=plenary-busted`, your existing `minimal_init.vim` is used verbatim via `--minimal-init`.

## Configuration

Every flag you can pass on the command line can also be set in a `neospec.toml` file at your project root, or via an environment variable. This is useful for CI setups where you want to keep the CLI invocation short and put the details in a checked-in config file.

Copy [`neospec.toml.example`](neospec.toml.example) as a starting point:

```toml
neovim_version = "stable"
test_patterns  = ["test/**/*_spec.lua"]
coverage_dir   = "coverage"
formats        = ["console", "lcov"]
threshold      = 80.0
verbose        = false

# Optional: bootstrap a runtime setup before tests load.
# init_file = "tests/minimal_init.lua"

# Optional: restrict coverage to your own source tree.
# coverage_include = ["lua/", "plugin/"]

# Optional: include untested files in the report so they can't be "silently 100%".
# coverage_source = ["lua/**/*.lua"]

# Optional: exact per-arm branch counts via source rewriting (opt-in, small cost).
# branch_instrumentation = false
```

### Precedence

Settings are resolved in this order (highest wins):

```
CLI flags  >  environment variables  >  neospec.toml  >  built-in defaults
```

### Environment variables

Every `NEOSPEC_*` variable overrides the same-named `neospec.toml` key.

| Variable | Purpose |
|---|---|
| `NEOSPEC_NEOVIM_VERSION` | Neovim version tag. |
| `NEOSPEC_TEST_PATTERNS` | Comma-separated glob patterns. |
| `NEOSPEC_COVERAGE_DIR` | Coverage output directory. |
| `NEOSPEC_FORMATS` | Comma-separated format list. |
| `NEOSPEC_CACHE_DIR` | Where cached Neovim binaries live. |
| `NEOSPEC_THRESHOLD` | Minimum coverage percentage. |
| `NEOSPEC_VERBOSE` | `true` or `1` for verbose output; `false` or `0` to disable. |
| `NEOSPEC_INIT_FILE` | Path to a Lua init file (analogous to `minimal_init.lua`). |
| `NEOSPEC_COVERAGE_INCLUDE` | Comma-separated path substrings — only include matching files in coverage. |
| `NEOSPEC_COVERAGE_SOURCE` | Comma-separated globs of files to include in the report even if untested. |
| `NEOSPEC_BRANCH_INSTRUMENTATION` | `true`/`1` to enable source-rewriting branch coverage. |

### Report formats at a glance

| Format | Flag value | Output file | Best consumed by |
|---|---|---|---|
| Color console summary | `console` | stdout | Humans reading CI logs. |
| LCOV tracefile | `lcov` | `coverage/lcov.info` | Codecov, Coveralls, `genhtml`, most badge tools. |
| Cobertura XML | `cobertura` | `coverage/cobertura.xml` | Azure DevOps, Jenkins, GitLab, SonarQube. |
| Coveralls JSON | `coveralls` | `coverage/coveralls.json` | Coveralls.io direct upload. |
| JUnit XML | `junit` | `coverage/junit.xml` | GitHub Actions test-result annotations, Jenkins, most CI dashboards. Not supported in `cover` mode. |

Multiple formats can be enabled at once — `neospec run --format=console --format=lcov --format=junit` writes to all three.

### Branch and function coverage

- Line coverage: `debug.sethook` records every executed Lua line. No source changes required.
- Branch coverage: neospec parses your Lua source, locates every branch decision, and reports per-arm hits. By default those counts are line-hit-derived (same line = shared count, so a `if x then A end` on one line reports its arm as "unknown" rather than falsely 100%). Turn on `--branch-instrumentation` to rewrite sources ahead of time and get exact per-arm counts. The cost is one parse + splice per source file at run start, plus one function call per arm execution at test time.
- Function coverage: neospec walks the same AST to recover real function names (`M.setup`, `parser.tokenize`, etc.) so LCOV's `FN/FNDA` records show meaningful names instead of `anonymous@42`.

## Neovim version management

neospec downloads Neovim release archives from [neovim/neovim releases](https://github.com/neovim/neovim/releases) and caches the extracted binary at:

```
~/.cache/neospec/<version>/<os>/<arch>/bin/nvim
```

On Windows the cache root follows `%LOCALAPPDATA%\neospec`.

Subsequent runs skip the download entirely. Use `neospec cache list` to see what's cached and `neospec cache clean` to reclaim disk space (see [`neospec cache`](#neospec-cache--manage-the-downloaded-neovim-binaries)).

## Platform support

| Platform | Architecture | Status |
|---|---|---|
| Linux | x86_64 | Supported |
| Linux | arm64 | Supported |
| macOS | x86_64 | Supported |
| macOS | arm64 (Apple Silicon) | Supported |
| Windows | x86_64 | Supported |

## Development

```bash
git clone https://github.com/jedi-knights/neospec
cd neospec
go mod download
go build ./...
go test ./...
```

### Git hooks

A pre-push hook is included in `.githooks/pre-push`. It runs `golangci-lint` before every push and blocks the push if any issues are found, keeping CI green.

Activate it once after cloning:

```bash
git config core.hooksPath .githooks
```

The hook requires `golangci-lint` v2 to be installed locally. If it is not found, the hook skips silently rather than blocking the push. Install it from [golangci-lint.run](https://golangci-lint.run/welcome/install/).

### Running tests

```bash
# Unit tests (no Neovim required)
go test ./...

# With race detector
go test -race ./...

# Specific package
go test ./internal/domain/... -v

# E2E tests that require a real Neovim install (gated by env var)
NEOSPEC_E2E=1 go test -run '^TestBranchInstrumentation_TrueE2E' ./internal/adapters/cover/...
```

### Building a release binary

```bash
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags="-s -w -X main.version=$(git describe --tags --always)" \
  -o neospec \
  ./cmd/neospec
```

### Project layout

```
cmd/neospec/
  main.go                   Entry point, dependency wiring.
  commands/
    run.go                  neospec run
    cover.go                neospec cover
    exec.go                 neospec exec
    cache.go                neospec cache list/clean
    version.go              neospec version

internal/
  config/                   TOML + env + flag merging.
  domain/                   Pure types and business logic (no I/O, stdlib only).
  ports/                    Consumer-defined interfaces.
  adapters/
    neovim/                 GitHub release download and binary cache.
    sandbox/                Per-run XDG environment isolation.
    runner/                 Test file discovery and Neovim subprocess execution.
    cover/                  cover-mode executor, source rewriter, branch attribution.
    reporter/               LCOV, Cobertura, JUnit, Coveralls, console.
    matrix/                 exec-mode multi-version executor.

internal/adapters/runner/lua/
  harness.lua               describe / it / before_each / after_each / assert.*
  coverage_hook.lua         debug.sethook installer + package.loaders shim.
  reporter.lua              JSON serializer (stdout → Go parser).
```

Everything points inward toward `internal/domain`. Domain imports nothing beyond the standard library.

### Code style

- Standard `gofmt` formatting.
- `go vet` and `staticcheck` must pass with no warnings.
- Functions ≤ 40 lines, cyclomatic complexity ≤ 7.
- No globals; all dependencies injected via constructors.

## Contributing

Contributions are welcome. Please open an issue before starting significant work so we can discuss the approach — see [`docs/adoption-strategy.md`](docs/adoption-strategy.md) for the design philosophy governing what fits in the tool and what doesn't.

## License

[MIT](./LICENSE)

---

<div align="center">
Made for the Neovim plugin community by <a href="https://github.com/jedi-knights">Jedi Knights</a>
</div>
