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
	nvimPath string
	sandboxF ports.SandboxFactory
	exec     ports.CommandRunner
	verbose  bool
	initFile string
}

// New creates a Runner.
//   - nvimPath: absolute path to the nvim binary obtained from NeovimProvider.Ensure.
//   - sandboxF: factory for creating per-run XDG sandboxes.
//   - exec: Strategy for running subprocesses; inject a fake in tests.
//   - verbose: whether to pass -V3 to nvim for diagnostic output.
//   - initFile: optional path to a Lua file executed before the coverage hook and
//     test harness. When non-empty, its dofile() call is the very first line of
//     the generated shim so the init file runs outside of coverage instrumentation.
func New(nvimPath string, sandboxF ports.SandboxFactory, exec ports.CommandRunner, verbose bool, initFile string) *Runner {
	return &Runner{
		nvimPath: nvimPath,
		sandboxF: sandboxF,
		exec:     exec,
		verbose:  verbose,
		initFile: initFile,
	}
}

// NewWithDefaultSandbox creates a Runner using the standard XDG sandbox factory
// and the real os/exec command runner. Use this in production code.
func NewWithDefaultSandbox(nvimPath string, verbose bool, initFile string) *Runner {
	return New(nvimPath, sandbox.NewFactory(), realCommandRunner{}, verbose, initFile)
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
		}
	}
	suite.Duration = time.Since(start)

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
type runOutput struct {
	Tests    []testJSON     `json:"tests"`
	Coverage []coverageJSON `json:"coverage"`
	Error    string         `json:"error,omitempty"`
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
	Path  string        `json:"path"`
	Lines coverageLines `json:"lines"`
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
	shim, err := buildShim(testFile, r.initFile)
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
		for lineStr, count := range fc.Lines {
			lineNo, err := strconv.Atoi(lineStr)
			if err != nil {
				return nil, nil, fmt.Errorf("coverage file %q: invalid line key %q: %w", fc.Path, lineStr, err)
			}
			fileCov.Lines[lineNo] = count
		}
		cov.Files = append(cov.Files, fileCov)
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
