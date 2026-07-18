<div align="center">

# neospec

**A self-contained test runner and coverage tool for Neovim plugins and distributions.**

[![CI](https://github.com/jedi-knights/neospec/actions/workflows/ci.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/ci.yml)
[![Release](https://github.com/jedi-knights/neospec/actions/workflows/release.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/release.yml)
[![GoReleaser](https://github.com/jedi-knights/neospec/actions/workflows/goreleaser.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/goreleaser.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Badge](https://github.com/jedi-knights/neospec/actions/workflows/badge.yaml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/badge.yaml)
[![Coverage](https://img.shields.io/badge/Coverage-91.6%25-brightgreen)](https://jedi-knights.github.io/neospec/?v=20)

[Installation](#installation) · [Quickstart](#quickstart) · [Writing Tests](#writing-tests) · [Coverage](#coverage) · [GitHub Action](#github-action) · [Configuration](#configuration) · [Contributing](#contributing)

</div>

---

neospec manages its own Neovim binary — no system install required. Point it at your test files and it handles the rest: isolated execution, line-level coverage instrumentation, and report generation in the formats your CI pipeline already understands.

```
$ neospec run

  ✓ parser > handles empty input
  ✓ parser > tokenizes identifiers
  ✗ parser > rejects invalid syntax
    expected error, got nil

Tests: 2 passed, 1 failed  (0.31s)
Coverage: 73.4% (142/193 lines)
```

## Why neospec?

**Runs your existing plenary.busted tests unchanged.** neospec's harness implements the same `describe` / `it` / `before_each` / `after_each` / `pending` DSL as [plenary.nvim](https://github.com/nvim-lua/plenary.nvim)'s busted harness, and the same `assert.equals` / `assert.is_true` / `assert.has_error` / `assert.matches` shape. Point neospec at your existing `tests/**/*_spec.lua` files and it just works — no rewrites, no migration, no vendored dependencies to keep in sync. See [Porting from plenary.busted](#porting-from-plenarybusted) for the small number of edge-case differences.

**Prefer not to switch runners at all?** `neospec cover` instruments plenary-busted or mini.test directly and gives you coverage reports without touching how the tests run. Your test framework stays. Your coverage story appears. See the [`neospec cover` reference](#neospec-cover--coverage-for-the-test-framework-you-already-use).

Beyond compatibility, neospec is a single binary that solves the CI-plumbing problems every Neovim plugin repo re-solves from scratch:

- **Downloads and caches Neovim automatically** from the official GitHub releases — the right version for your OS and architecture, every time
- **Isolates every test run** in a clean XDG environment so your tests cannot read or mutate your real Neovim config
- **Instruments Lua coverage** via `debug.sethook` with no changes to your code
- **Emits reports** in LCOV, Cobertura XML, JUnit XML, and a color console summary — the formats your CI parser and badge generator already accept
- **Wraps existing runners** via `neospec cover` — plenary-busted and mini.test users get coverage without swapping test frameworks

The alternative — installing Neovim system-wide, vendoring [busted](https://lunarmodules.github.io/busted/) or plenary, or writing fragile shell scripts around `nvim --headless` — never quite works cleanly in ephemeral CI environments. neospec is what those shell scripts should have been.

## Installation

### Homebrew (macOS and Linux)

```bash
brew install jedi-knights/tap/neospec
```

### Pre-built binaries

Download the latest release for your platform from the [Releases page](https://github.com/jedi-knights/neospec/releases).

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

## Quickstart

```bash
# Run tests (discovers test/**/*_spec.lua by default)
neospec run

# Emit LCOV and console output, fail if coverage drops below 80%
neospec run --format=console --format=lcov --threshold=80

# Pin a specific Neovim version
neospec run --neovim-version=v0.10.4

```

## Writing Tests

neospec ships a minimal BDD harness inspired by RSpec and busted. Test files are discovered by glob pattern (default: `test/**/*_spec.lua`).

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

| Assertion | Description |
|:---|:---|
| `assert.equals(expected, actual [, msg])` | Strict equality (`==`) |
| `assert.not_equals(unexpected, actual [, msg])` | Strict inequality |
| `assert.is_true(v [, msg])` | Value is exactly `true` |
| `assert.is_false(v [, msg])` | Value is exactly `false` |
| `assert.is_nil(v [, msg])` | Value is `nil` |
| `assert.is_not_nil(v [, msg])` | Value is not `nil` |
| `assert.has_error(fn [, msg])` | Calling `fn` raises an error |
| `assert.matches(pattern, str [, msg])` | String matches a Lua pattern |

All assertions accept an optional final `msg` argument that overrides the default failure message.

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

Every file that runs under `PlenaryBustedDirectory` runs under `neospec run` with no source changes — the DSL is deliberately API-compatible. A handful of narrow semantic gaps exist where neospec's harness intentionally differs from [luassert](https://github.com/lunarmodules/luassert) (the assertion library plenary.busted vendors); each is called out below with its workaround.

### Assertion differences

| Method | plenary.busted / luassert | neospec |
|:---|:---|:---|
| `assert.matches(pattern, str)` | Lua pattern by default; PCRE via `.re` | Lua pattern only |
| `assert.same(expected, actual)` | Deep-equal via luassert | Not implemented — use `assert.equals` for scalars; walk table structure explicitly for deep comparisons |
| `assert.truthy(v)` / `assert.falsy(v)` | Loose truthiness (any non-`nil`, non-`false` value passes) | Use `assert.is_true` / `assert.is_false` for strict boolean checks |
| `assert.spy(fn).was.called()` | Spy/mock via luassert's `spy` module | Not implemented — inject test doubles at construction sites (dependency-injection pattern) |
| `assert.stub(mod, "fn").returns(v)` | Stub via luassert's `stub` module | Not implemented — same reasoning as spies |

If your suite uses `assert.same` or the spy/stub API extensively, prefer [`neospec cover`](#neospec-cover--coverage-for-the-test-framework-you-already-use) over `neospec run` — cover keeps your existing plenary invocation and just adds coverage instrumentation on top.

### Discovery differences

- plenary.busted matches `*_spec.lua` under any directory you name at the `PlenaryBustedDirectory` call site; neospec defaults to `test/**/*_spec.lua` but accepts arbitrary glob patterns via `--pattern` (repeatable).
- `PlenaryBustedFile <file>` maps to `neospec run --pattern=<file>`.

### Bootstrapping differences

- plenary.busted requires a `minimal_init.vim` (or `.lua`) that puts plenary on the runtimepath before tests run; neospec's own harness is embedded in the binary so no runtimepath bootstrap is needed for `neospec run`. If you use `neospec cover --runner=plenary-busted`, your existing `minimal_init.vim` is used verbatim via `--minimal-init`.

## Coverage

Coverage is collected via Lua's `debug.sethook` API. The hook fires on every executed line and records the hit count. No source transformation or annotation is required. If you'd rather keep your existing test runner and only add coverage on top, see [`neospec cover`](#neospec-cover--coverage-for-the-test-framework-you-already-use) — it wraps plenary-busted, mini.test, or an arbitrary external command and produces the same LCOV / Cobertura / Coveralls / console reports without changing how your tests run.

### Reading the console output

```
Coverage: 87.5% (175/200 lines)
```

For per-file breakdowns, use `--format=lcov` and open the report in your editor or coverage service.

### Supported report formats

| Format | Flag value | Output file |
|:---|:---|:---|
| Color console summary | `console` | stdout |
| LCOV tracefile | `lcov` | `coverage/lcov.info` |
| Cobertura XML | `cobertura` | `coverage/cobertura.xml` |
| Coveralls JSON | `coveralls` | `coverage/coveralls.json` |
| JUnit XML | `junit` | `coverage/junit.xml` |

Multiple formats can be enabled simultaneously:

```bash
neospec run --format=console --format=lcov --format=junit
```

### Coverage threshold

Fail the run when coverage falls below a minimum:

```bash
neospec run --threshold=80
# exits non-zero if coverage < 80%
```

Useful as a CI gate to prevent coverage regressions from merging.

## GitHub Action

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
          neovim-version: stable      # stable | nightly | v0.10.4
          formats: console,lcov       # comma-separated
          threshold: "80"             # fail if coverage < 80%

      - uses: jedi-knights/coverage-badge@v1   # update README badge
```

### Action inputs

| Input | Default | Description |
|:---|:---|:---|
| `neovim-version` | `stable` | Neovim version: `stable`, `nightly`, or semver like `0.10.4` |
| `test-patterns` | `test/**/*_spec.lua` | Comma-separated glob patterns for test discovery |
| `coverage-dir` | `coverage` | Directory for coverage report files |
| `formats` | `console` | Comma-separated output formats |
| `threshold` | `0` | Minimum coverage percentage; `0` disables the check |
| `verbose` | `false` | Enable verbose output |

## Configuration

Create `neospec.toml` in your project root (or copy from `neospec.toml.example`):

```toml
neovim_version = "stable"
test_patterns  = ["test/**/*_spec.lua"]
coverage_dir   = "coverage"
formats        = ["console", "lcov"]
threshold      = 80.0
verbose        = false
```

### Precedence

Settings are resolved in this order (highest wins):

```
CLI flags  >  environment variables  >  neospec.toml  >  built-in defaults
```

### Environment variables

| Variable | Description |
|:---|:---|
| `NEOSPEC_NEOVIM_VERSION` | Neovim version tag |
| `NEOSPEC_TEST_PATTERNS` | Comma-separated glob patterns |
| `NEOSPEC_COVERAGE_DIR` | Coverage output directory |
| `NEOSPEC_FORMATS` | Comma-separated format list |
| `NEOSPEC_VERBOSE` | `true` or `1` for verbose output |

## CLI reference

```
neospec [command]

Commands:
  run           Discover and run test files, collect coverage, emit reports
  exec          Run a command against multiple Neovim versions
  cover         Collect coverage while running plenary-busted, mini.test, or an external command
  version       Print neospec version and exit
  cache list    List cached Neovim versions and their sizes on disk
  cache clean   Remove all cached Neovim binaries

Flags (run):
  -c, --config string           path to config file (default "neospec.toml")
      --neovim-version string   neovim version to use
      --pattern stringArray     glob pattern(s) for test files (repeatable)
      --format stringArray      output format(s) (repeatable)
      --coverage-dir string     directory for coverage report files
      --threshold float         minimum coverage percentage
      --cache-dir string        directory for cached Neovim binaries
  -v, --verbose                 verbose output

Flags (exec):
  -c, --config string           path to config file (default "neospec.toml")
      --versions string         comma-separated Neovim versions (e.g. stable,nightly,v0.10.4)
      --format string           output format: console, json (default "console")
      --cache-dir string        directory for cached Neovim binaries
  -v, --verbose                 verbose output

Flags (cover):
  -c, --config string           path to config file (default "neospec.toml")
      --runner string           wrapped runner: plenary-busted, mini-test, or external
      --dir string              test directory or glob (required for plenary-busted and mini-test)
      --minimal-init string     path to init file that bootstraps plenary or mini.test
      --neovim-version string   neovim version to use
      --format stringArray      output format(s): console, lcov, cobertura, coveralls (repeatable)
      --coverage-dir string     directory for coverage report files
      --threshold float         minimum coverage percentage (0 = disabled)
      --cache-dir string        directory for cached Neovim binaries
  -v, --verbose                 verbose output
```

### `neospec cover` — coverage for the test framework you already use

`cover` instruments your existing test framework with Lua-level coverage collection **without replacing the runner**. Use it when you're already invested in `plenary.nvim`'s `PlenaryBustedDirectory` or `mini.test` and want coverage reports (LCOV, Cobertura, Coveralls, console) added to your CI without rewriting a single test.

The mechanism is transparent to the wrapped runner: neospec builds a Lua shim that installs the coverage hook, wires a `VimLeavePre` autocmd to serialize the collected data to a file on the way out, and invokes your runner programmatically. Your existing `tests/minimal_init.vim` (or `.lua`) is used verbatim as the runtimepath bootstrap — no changes needed to how your tests load.

```bash
# plenary-busted — the tj-ecosystem's default
neospec cover --runner=plenary-busted --dir=tests/ \
  --minimal-init=tests/minimal_init.vim \
  --format=console --format=lcov --threshold=80

# mini.test
neospec cover --runner=mini-test --dir=tests/ \
  --minimal-init=scripts/minimal_init.lua \
  --format=lcov

# External mode — your Makefile drives nvim, cover just adds coverage
neospec cover --runner=external --format=lcov -- make test
```

For **plenary-busted** and **mini-test** modes, the wrapped runner is invoked from inside the shim; `--dir` is the directory (or glob) the runner scans. Exit code is non-zero if either the runner fails or the collected coverage falls below `--threshold`.

For **external** mode, cover sets `NEOSPEC_COVER_HOOK` and `NEOSPEC_COVER_OUTPUT` env vars and runs your command. Your command is responsible for loading the hook (e.g. `nvim -c "luafile $NEOSPEC_COVER_HOOK" ...`) and ensuring the reporter fires before nvim exits. This is the escape hatch for CI setups that already have a `make test` target you don't want to reshape around neospec's opinion.

Cover mode intentionally does not support `--format=junit` — cover has no test-suite data to serialize into JUnit's schema. If you need JUnit output alongside coverage, use `neospec run` on a neospec-native test file, or emit JUnit from your existing runner's own reporter.

### `neospec exec` — run a command across a Neovim version matrix

Use `exec` to run an arbitrary command once per Neovim version and aggregate the outcome. Each version's Neovim binary is placed first on `PATH` so `nvim` inside your command resolves to that specific build; the same sandboxed XDG environment `run` uses is applied so plugin invocations cannot read or mutate your real Neovim configuration.

`--` separates neospec's own flags from the wrapped command:

```bash
# Sanity-check that both stable and nightly launch without errors
neospec exec --versions=stable,nightly -- nvim --version

# Run a plenary-busted suite against three Neovim versions
neospec exec --versions=stable,nightly,v0.10.4 -- \
  nvim --headless -c "PlenaryBustedDirectory tests/" -c "qa!"
```

The exit code is non-zero if any version fails. JSON output (`--format=json`) makes it straightforward to consume the matrix results in CI without parsing console text:

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

## Neovim version management

neospec downloads Neovim release archives from [neovim/neovim releases](https://github.com/neovim/neovim/releases) and caches the extracted binary at:

```
~/.cache/neospec/<version>/<os>/<arch>/bin/nvim
```

On Windows the cache root follows `%LOCALAPPDATA%\neospec`.

Subsequent runs skip the download entirely. To see what is cached:

```bash
neospec cache list

VERSION               SIZE
-------               ----
stable                28.4 MB
v0.10.4               27.1 MB
```

To free disk space:

```bash
neospec cache clean
```

## Platform support

| Platform | Architecture | Status |
|:---|:---|:---|
| Linux | x86_64 | ✓ Supported |
| Linux | arm64 | ✓ Supported |
| macOS | x86_64 | ✓ Supported |
| macOS | arm64 (Apple Silicon) | ✓ Supported |
| Windows | x86_64 | ✓ Supported |

## Contributing

Contributions are welcome. Please open an issue before starting significant work so we can discuss the approach.

### Development setup

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
  main.go                   Entry point, dependency wiring
  commands/
    run.go                  neospec run
    version.go              neospec version
    cache.go                neospec cache list/clean

internal/
  config/                   TOML + env + flag merging
  domain/                   Pure types and business logic (no I/O, no external deps)
  ports/                    Consumer-defined interfaces
  adapters/
    neovim/                 GitHub release download and binary cache
    sandbox/                Per-run XDG environment isolation
    runner/                 Test file discovery and Neovim subprocess execution
    reporter/               LCOV, Cobertura, JUnit, console

internal/adapters/runner/lua/
  harness.lua               describe / it / before_each / after_each / assert.*
  coverage_hook.lua         debug.sethook installer and data collector
  reporter.lua              JSON serializer (stdout → Go parser)
```

The dependency rule: everything points inward toward `internal/domain`. Domain imports nothing beyond the standard library.

### Code style

- Standard `gofmt` formatting
- `go vet` and `staticcheck` must pass with no warnings
- Functions ≤ 40 lines, cyclomatic complexity ≤ 7
- No globals; all dependencies injected via constructors

## License

[MIT](./LICENSE)

---

<div align="center">
Made for the Neovim plugin community by <a href="https://github.com/jedi-knights">Jedi Knights</a>
</div>
