<div align="center">

# neospec

**A self-contained test runner and coverage tool for Neovim plugins and distributions.**

[![CI](https://github.com/jedi-knights/neospec/actions/workflows/ci.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/ci.yml)
[![Release](https://github.com/jedi-knights/neospec/actions/workflows/release.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/release.yml)
[![GoReleaser](https://github.com/jedi-knights/neospec/actions/workflows/goreleaser.yml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/goreleaser.yml)
[![ReportCard](https://goreportcard.com/badge/github.com/jedi-knights/neospec?style=flat)](https://goreportcard.com/report/github.com/jedi-knights/neospec)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Badge](https://github.com/jedi-knights/neospec/actions/workflows/badge.yaml/badge.svg)](https://github.com/jedi-knights/neospec/actions/workflows/badge.yaml)
[![Coverage](https://img.shields.io/badge/Coverage-92.4%25-brightgreen)](https://jedi-knights.github.io/neospec/?v=11)

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

Testing Neovim plugins typically requires either installing Neovim system-wide, vendoring a test framework like [busted](https://lunarmodules.github.io/busted/) or [plenary.nvim](https://github.com/nvim-lua/plenary.nvim), or writing fragile shell scripts that shell out to `nvim --headless`. None of these work cleanly in ephemeral CI environments.

neospec is a single binary that:

- **Downloads and caches Neovim automatically** from the official GitHub releases — the right version for your OS and architecture, every time
- **Isolates every test run** in a clean XDG environment so your tests cannot read or mutate your real Neovim config
- **Instruments Lua coverage** via `debug.sethook` with no changes to your code
- **Emits reports** in LCOV, Cobertura XML, JUnit XML, and a color console summary — the formats your CI parser and badge generator already accept

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

## Coverage

Coverage is collected via Lua's `debug.sethook` API. The hook fires on every executed line and records the hit count. No source transformation or annotation is required.

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

      - uses: jedi-knights/neospec@v1
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
