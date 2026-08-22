package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// Runner executes test files in headless Neovim subprocesses.
type Runner struct {
	nvimPath        string
	sandboxF        ports.SandboxFactory
	exec            ports.CommandRunner
	verbose         bool
	initFile        string
	coverageInclude []string
	coverageSources []string
	// resolvedSources is populated once per Run from coverageSources so the
	// glob walk does not repeat for every test file.
	resolvedSources []string
	// funcNamesFn, when non-nil, resolves a per-file function-name map for
	// the resolved coverage sources. The Runner emits its result as a
	// _neospec_function_names Lua global so the coverage hook can look up
	// definition names by (path, line). Set by WithFunctionNameResolver;
	// when nil the runner emits nothing and every function label in the
	// report falls back to "anonymous@N" — real names require configuring
	// coverage_source so the Go side can pre-process the file.
	funcNamesFn   FunctionNameResolver
	functionNames map[string]map[int]string
	// srcRewriterFn, when non-nil, resolves per-file rewritten source for
	// branch instrumentation. The Runner emits the result as a
	// _neospec_rewritten_sources Lua global; the coverage hook's package.
	// loaders shim serves those bytes under the original file path so
	// debug.getinfo(...).source still resolves correctly. See
	// docs/branch-instrumentation-design.md.
	srcRewriterFn    SourceRewriteResolver
	rewrittenSources map[string]string
	// injections is the opaque metadata the resolver returned alongside
	// the source map. Exposed via Injections() so the CLI can hand it to
	// cover.ApplyBranchCounters after Run(). Held as any to keep this
	// package from importing cover (which imports runner — the same
	// cycle avoidance as FunctionNameResolver).
	injections any
	// branchCounts is the aggregated _neospec_br_counts across every
	// test-file subprocess. Populated as parseOutput reads each
	// subprocess's JSON; summed rather than replaced so require()'d
	// modules accumulate counts across the files that exercised them.
	branchCounts map[int]int
}

// FunctionNameResolver returns a two-level (path, line) → name map for
// the given resolved source paths. Implementation lives in
// internal/adapters/cover.FunctionNameMap; kept behind an interface here
// so this package does not import cover (which itself imports runner —
// avoiding a cycle).
type FunctionNameResolver func(paths []string) map[string]map[int]string

// SourceRewriteResolver returns rewritten source for each of the given
// paths (as {path → rewritten_source}), plus opaque metadata that
// Runner.Injections() surfaces after Run() so the CLI can attribute
// runtime counter values back to source positions. Implementation lives
// in internal/adapters/cover.RewriteAll; kept behind an interface here
// for the same cycle-avoidance reason as FunctionNameResolver.
//
// The metadata is opaque to the runner (returned as any) so we can
// evolve the injection type without churning the runner API.
type SourceRewriteResolver func(paths []string) (map[string]string, any)

// New creates a Runner.
//   - nvimPath: absolute path to the nvim binary obtained from NeovimProvider.Ensure.
//   - sandboxF: factory for creating per-run XDG sandboxes.
//   - exec: Strategy for running subprocesses; inject a fake in tests.
//   - verbose: whether to pass -V3 to nvim for diagnostic output.
//   - initFile: optional path to a Lua file executed before the coverage hook and
//     test harness. When non-empty, its dofile() call is the very first line of
//     the generated shim so the init file runs outside of coverage instrumentation.
//   - coverageInclude: optional list of path substrings. When non-empty, the
//     coverage hook only records source files whose path contains at least one
//     of these strings, restricting coverage to the plugin's own source tree.
func New(nvimPath string, sandboxF ports.SandboxFactory, exec ports.CommandRunner, verbose bool, initFile string, coverageInclude []string) *Runner {
	return &Runner{
		nvimPath:        nvimPath,
		sandboxF:        sandboxF,
		exec:            exec,
		verbose:         verbose,
		initFile:        initFile,
		coverageInclude: coverageInclude,
	}
}

// NewWithDefaultSandbox creates a Runner using the standard XDG sandbox factory
// and the real os/exec command runner. Use this in production code.
func NewWithDefaultSandbox(nvimPath string, verbose bool, initFile string, coverageInclude []string) *Runner {
	return New(nvimPath, sandbox.NewFactory(), realCommandRunner{}, verbose, initFile, coverageInclude)
}

// resolveCoverageSources expands the configured globs to absolute paths.
//
// A resolution failure must not fail the run: coverage is a report, not a
// gate, so a bad glob degrades to hook-observed files only rather than
// aborting a suite that otherwise passed.
func (r *Runner) resolveCoverageSources(ctx context.Context) []string {
	if len(r.coverageSources) == 0 {
		return nil
	}
	found, err := Discover(ctx, r.coverageSources)
	if err != nil {
		return nil
	}
	abs := make([]string, 0, len(found))
	for _, f := range found {
		a, aerr := filepath.Abs(f)
		if aerr != nil {
			continue
		}
		abs = append(abs, a)
	}
	return abs
}

// WithCoverageSources sets glob patterns for source files that should appear
// in the coverage report even when no test loads them.
//
// Supplied as a method rather than a seventh constructor parameter: New is
// already at six and every call site would have to change to opt out of a
// feature most callers do not use.
//
// Patterns use the same syntax as test discovery (including "**"), because
// coverage_include is a substring filter and cannot be walked to find files.
// Returns the receiver for chaining.
func (r *Runner) WithCoverageSources(patterns []string) *Runner {
	r.coverageSources = patterns
	return r
}

// WithFunctionNameResolver sets a callback that returns a (path, line) →
// name map for the resolved coverage sources. When set, the runner emits
// the result as a _neospec_function_names Lua global so the coverage hook
// can look up definition names by (path, line). Wire in
// cover.FunctionNameMap from the CLI.
//
// Nil is the default: no global emitted; every function label falls back
// to "anonymous@N". Real names require configuring coverage_source so the
// Go side can pre-process the file.
func (r *Runner) WithFunctionNameResolver(fn FunctionNameResolver) *Runner {
	r.funcNamesFn = fn
	return r
}

// WithSourceRewriter sets a callback that produces rewritten Lua source
// for the resolved coverage sources, plus opaque injection metadata for
// post-run attribution. When set, the runner emits the rewritten source
// as a _neospec_rewritten_sources Lua global; the coverage hook's
// package.loaders shim serves those bytes for each known path under the
// original file name. Wire in cover.RewriteAll from the CLI.
//
// The metadata is available via Injections() after Run completes; hand
// it to cover.ApplyBranchCounters along with BranchCounts() to attribute
// runtime counters to arms.
//
// Nil is the default and disables branch instrumentation: no global is
// emitted, no source is rewritten, and every file loads via Neovim's
// default loader.
func (r *Runner) WithSourceRewriter(fn SourceRewriteResolver) *Runner {
	r.srcRewriterFn = fn
	return r
}

// Injections returns the opaque metadata produced by the source-rewrite
// resolver during the last Run. Nil until Run has been called and only
// non-nil when WithSourceRewriter was set. Callers type-assert to their
// expected concrete type (typically []cover.Injection).
func (r *Runner) Injections() any {
	return r.injections
}

// BranchCounts returns the aggregated _neospec_br_counts map across
// every test-file subprocess run in the last Run. Nil when no
// instrumentation was active or no counter fired. Sum semantics: a
// counter that fires in two subprocesses (e.g., via a require()'d
// module exercised by two test files) is reported as the sum of both
// subprocesses' hits.
func (r *Runner) BranchCounts() map[int]int {
	return r.branchCounts
}

// mergeBranchCounts sums a subprocess's per-branch counter map into
// r.branchCounts. Extracted from Run for cyclomatic-complexity budget
// reasons and to make the sum semantics grep-able.
func (r *Runner) mergeBranchCounts(counts map[int]int) {
	if len(counts) == 0 {
		return
	}
	if r.branchCounts == nil {
		r.branchCounts = make(map[int]int, len(counts))
	}
	for id, hits := range counts {
		r.branchCounts[id] += hits
	}
}

// Discover satisfies the discovery half of ports.TestRunner.
func (r *Runner) Discover(ctx context.Context, patterns []string) ([]string, error) {
	return Discover(ctx, patterns)
}

// Run executes each test file in parallel, aggregates results and coverage, and
// returns them in the same order as files. Workers are capped at runtime.NumCPU()
// so the test suite uses available cores without oversubscribing.
func (r *Runner) Run(ctx context.Context, files []string) (*domain.SuiteResult, *domain.CoverageData, error) {
	n := len(files)
	if n == 0 {
		return &domain.SuiteResult{}, &domain.CoverageData{}, nil
	}

	type runResult struct {
		idx     int
		suite   *domain.SuiteResult
		cov     *domain.CoverageData
		err     error
		skipped bool // true when the worker skipped this index due to context cancellation
	}

	// Resolve coverage source globs once, before any worker starts, so the
	// walk happens a single time and the workers see a stable slice.
	r.resolvedSources = r.resolveCoverageSources(ctx)

	// Extract function names from the resolved sources once, before any
	// worker starts, so every subprocess's shim carries the same map.
	// Cheap: parsing is fast and the map is shared across workers.
	if r.funcNamesFn != nil {
		r.functionNames = r.funcNamesFn(r.resolvedSources)
	}

	// Rewrite source for branch instrumentation once, before any worker
	// starts, so every subprocess uses byte-identical rewritten bytes
	// and BranchIDs stay stable across subprocess boundaries. Reset
	// per-Run state so a Runner reused across Runs does not carry
	// stale injections or counters into the next attribution pass.
	r.injections = nil
	r.branchCounts = nil
	r.rewrittenSources = nil
	if r.srcRewriterFn != nil {
		r.rewrittenSources, r.injections = r.srcRewriterFn(r.resolvedSources)
	}

	// Feed file indices to workers via a buffered jobs channel.
	jobs := make(chan int, n)
	for i := range n {
		jobs <- i
	}
	close(jobs)

	resultsCh := make(chan runResult, n)

	numWorkers := min(runtime.NumCPU(), n)

	// Start the timer before launching workers so suite.Duration reflects the
	// full wall-clock time including goroutine startup and first-job pickup.
	start := time.Now()

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					// Context cancelled — skip remaining jobs as a best-effort early
					// exit. Note: this check is non-atomic; runOne may still be called
					// for a job that arrives in the window between this check and the
					// dispatch below. runOne propagates ctx, so any result it records
					// will carry context-cancellation context. Run() surfaces ctx.Err()
					// at the return site so callers can distinguish abort from test failure.
					// Send a skipped marker so the consumer always receives n results and
					// does not need to rely on the (nil, nil) zero-value to detect gaps.
					resultsCh <- runResult{idx: idx, skipped: true}
					continue
				}
				suite, cov, err := r.runOne(ctx, files[idx])
				resultsCh <- runResult{idx: idx, suite: suite, cov: cov, err: err}
			}
		}()
	}
	go func() { wg.Wait(); close(resultsCh) }()

	// Collect into an ordered slice so output is deterministic regardless of
	// which worker finishes first.
	ordered := make([]runResult, n)
	for res := range resultsCh {
		ordered[res.idx] = res
	}

	suite := &domain.SuiteResult{}
	cov := &domain.CoverageData{}
	for _, res := range ordered {
		if res.skipped {
			// Worker skipped this index due to context cancellation; ctx.Err()
			// is returned below so callers know the run was aborted.
			continue
		}
		if res.err != nil {
			// Record the error as a test failure rather than aborting the run.
			suite.Tests = append(suite.Tests, domain.TestResult{
				Name:   files[res.idx],
				Status: domain.StatusError,
				Error:  res.err.Error(),
			})
			continue
		}
		suite.Tests = append(suite.Tests, res.suite.Tests...)
		if res.cov != nil {
			cov.Files = append(cov.Files, res.cov.Files...)
			r.mergeBranchCounts(res.cov.BranchCounters)
		}
	}
	suite.Duration = time.Since(start)

	// Each subprocess reports coverage for every file it touched, so the same
	// path arrives once per test file. Merge before returning; otherwise both
	// totals and hits are multiplied by however many subprocesses happened to
	// load each file, and the resulting percentage is meaningless.
	cov.Normalize()

	// Surface the aggregated branch counts on the returned CoverageData
	// too, so callers that don't retain a Runner reference (e.g. tests
	// using ParseReporterOutput directly) still see them.
	cov.BranchCounters = r.branchCounts

	// Propagate context cancellation so callers can distinguish "the run was
	// aborted" from "all test files failed normally".
	if err := ctx.Err(); err != nil {
		return suite, cov, err
	}
	return suite, cov, nil
}

// runOutput is the JSON structure that the Lua harness writes to stdout.
// The Error field is populated by reporter.lua's pcall guard when the
// serialisation fails; if non-empty, it indicates a harness-level failure
// and parseOutput surfaces it as a Go error rather than silently returning
// an empty suite and coverage.
//
// BranchCounts is populated only when branch instrumentation was active
// AND at least one counter fired — reporter.lua omits the field on empty
// counts so a "field absent" state is distinguishable from "instrumented
// but nothing hit" (empty map).
type runOutput struct {
	Tests        []testJSON     `json:"tests"`
	Coverage     []coverageJSON `json:"coverage"`
	BranchCounts map[int]int    `json:"br_counts,omitempty"`
	Error        string         `json:"error,omitempty"`
}

type testJSON struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	DurationMs float64 `json:"duration_ms"`
	Output     string  `json:"output"`
	Error      string  `json:"error"`
}

// coverageLines is map[string]int that gracefully handles the case where the
// Lua reporter emits an empty JSON array ("[]") instead of an empty JSON object
// ("{}"). Lua's built-in table encoder has no way to distinguish between an
// empty array and an empty object; rather than crash, we treat "[]" as no data.
type coverageLines map[string]int

func (cl *coverageLines) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[]")) {
		*cl = nil // empty array → treat as no coverage data
		return nil
	}
	var m map[string]int
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*cl = m
	return nil
}

type coverageJSON struct {
	Path      string         `json:"path"`
	Lines     coverageLines  `json:"lines"`
	Functions []functionJSON `json:"functions,omitempty"`
}

type functionJSON struct {
	Name  string `json:"name"`
	Line  int    `json:"line"`
	Count int    `json:"count"`
}

// runOne executes a single test file in a fresh sandbox.
func (r *Runner) runOne(ctx context.Context, testFile string) (suite *domain.SuiteResult, cov *domain.CoverageData, retErr error) {
	sb, err := r.sandboxF.Create(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("creating sandbox: %w", err)
	}
	// Join any close error into retErr so temp-dir cleanup failures surface
	// as visible errors rather than being silently discarded.
	defer func() {
		if cerr := sb.Close(); cerr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("closing sandbox: %w", cerr))
		}
	}()

	// Write the combined harness+hook Lua shim into the sandbox.
	shimPath := filepath.Join(sb.Dir(), "neospec_run.lua")
	shim, err := buildShim(testFile, r.initFile, r.coverageInclude, r.resolvedSources, r.functionNames, r.rewrittenSources)
	if err != nil {
		return nil, nil, fmt.Errorf("building shim: %w", err)
	}
	if err := os.WriteFile(shimPath, shim, 0o644); err != nil {
		return nil, nil, fmt.Errorf("writing shim: %w", err)
	}

	args := []string{"--headless", "-l", shimPath}
	if r.verbose {
		args = append([]string{"-V3"}, args...)
	}

	stdout, stderr, err := r.exec.Run(ctx, sb.Env(), r.nvimPath, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("nvim exited with error: %w (stderr: %.500s)", err, stderr)
	}

	suite, cov, retErr = parseOutput(stdout)
	return
}

// ParseReporterOutput decodes the JSON emitted by the Lua reporter into
// domain types. It is the public interface companion adapters (like the
// coverage-only wrapper around plenary-busted or mini.test) use to consume
// the reporter's output after intercepting it from stdout or a file.
func ParseReporterOutput(data []byte) (*domain.SuiteResult, *domain.CoverageData, error) {
	return parseOutput(data)
}

// parseOutput decodes the JSON emitted by the Lua harness.
func parseOutput(data []byte) (*domain.SuiteResult, *domain.CoverageData, error) {
	var out runOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, nil, fmt.Errorf("parsing harness output: %w (raw: %.200s)", err, string(data))
	}
	if out.Error != "" {
		return nil, nil, fmt.Errorf("lua reporter error: %s", out.Error)
	}

	suite := &domain.SuiteResult{}
	for _, t := range out.Tests {
		suite.Tests = append(suite.Tests, domain.TestResult{
			Name:     t.Name,
			Status:   parseStatus(t.Status),
			Duration: time.Duration(t.DurationMs * float64(time.Millisecond)),
			Output:   t.Output,
			Error:    t.Error,
		})
	}

	cov := &domain.CoverageData{}
	for _, fc := range out.Coverage {
		if len(fc.Lines) == 0 {
			// Skip entries with no line data — the Lua reporter may emit
			// "lines":[] for files that were loaded but had no recorded hits.
			continue
		}
		fileCov := &domain.FileCoverage{
			Path:  fc.Path,
			Lines: make(map[int]int, len(fc.Lines)),
		}
		for _, fn := range fc.Functions {
			fileCov.Functions = append(fileCov.Functions, domain.FunctionCoverage{
				Name:  fn.Name,
				Line:  fn.Line,
				Count: fn.Count,
			})
		}
		for lineStr, count := range fc.Lines {
			lineNo, err := strconv.Atoi(lineStr)
			if err != nil {
				return nil, nil, fmt.Errorf("coverage file %q: invalid line key %q: %w", fc.Path, lineStr, err)
			}
			fileCov.Lines[lineNo] = count
		}
		cov.Files = append(cov.Files, fileCov)
	}

	// Branch instrumentation counters. Reporter emits the field only
	// when the map is non-empty, so a nil out.BranchCounts means either
	// instrumentation was off or every counter was zero — either way
	// there is nothing to attribute.
	if len(out.BranchCounts) > 0 {
		cov.BranchCounters = out.BranchCounts
	}

	return suite, cov, nil
}

func parseStatus(s string) domain.TestStatus {
	switch s {
	case "pass":
		return domain.StatusPass
	case "fail":
		return domain.StatusFail
	case "skip":
		return domain.StatusSkip
	default:
		return domain.StatusError
	}
}
