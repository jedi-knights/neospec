package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jedi-knights/neospec/internal/adapters/sandbox"
	"github.com/jedi-knights/neospec/internal/domain"
	"github.com/jedi-knights/neospec/internal/ports"
)

// Runner executes test files in headless Neovim subprocesses.
type Runner struct {
	nvimPath string
	sandboxF ports.SandboxFactory
	verbose  bool
}

// New creates a Runner.
//   - nvimPath: absolute path to the nvim binary obtained from NeovimProvider.Ensure.
//   - sandboxF: factory for creating per-run XDG sandboxes.
//   - verbose: whether to pass -V3 to nvim for diagnostic output.
func New(nvimPath string, sandboxF ports.SandboxFactory, verbose bool) *Runner {
	return &Runner{
		nvimPath: nvimPath,
		sandboxF: sandboxF,
		verbose:  verbose,
	}
}

// NewWithDefaultSandbox creates a Runner using the standard XDG sandbox factory.
func NewWithDefaultSandbox(nvimPath string, verbose bool) *Runner {
	return New(nvimPath, sandbox.NewFactory(), verbose)
}

// Discover satisfies the discovery half of ports.TestRunner.
func (r *Runner) Discover(ctx context.Context, patterns []string) ([]string, error) {
	return Discover(ctx, patterns)
}

// Run executes each test file, aggregates results and coverage, and returns them.
func (r *Runner) Run(ctx context.Context, files []string) (*domain.SuiteResult, *domain.CoverageData, error) {
	suite := &domain.SuiteResult{}
	cov := &domain.CoverageData{}

	start := time.Now()
	for _, f := range files {
		res, fileCov, err := r.runOne(ctx, f)
		if err != nil {
			// Record the error as a test failure rather than aborting the run.
			suite.Tests = append(suite.Tests, domain.TestResult{
				Name:   f,
				Status: domain.StatusError,
				Error:  err.Error(),
			})
			continue
		}
		suite.Tests = append(suite.Tests, res.Tests...)
		cov.Files = append(cov.Files, fileCov.Files...)
	}
	suite.Duration = time.Since(start)
	return suite, cov, nil
}

// runOutput is the JSON structure that the Lua harness writes to stdout.
type runOutput struct {
	Tests    []testJSON    `json:"tests"`
	Coverage []coverageJSON `json:"coverage"`
}

type testJSON struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	DurationMs float64 `json:"duration_ms"`
	Output   string  `json:"output"`
	Error    string  `json:"error"`
}

type coverageJSON struct {
	Path  string         `json:"path"`
	Lines map[string]int `json:"lines"` // string keys because JSON object keys must be strings
}

// runOne executes a single test file in a fresh sandbox.
func (r *Runner) runOne(ctx context.Context, testFile string) (*domain.SuiteResult, *domain.CoverageData, error) {
	sb, err := r.sandboxF.Create(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("creating sandbox: %w", err)
	}
	defer sb.Close()

	// Write the combined harness+hook Lua shim into the sandbox.
	shimPath := filepath.Join(sb.Dir(), "neospec_run.lua")
	shim, err := buildShim(testFile)
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(shimPath, shim, 0o644); err != nil {
		return nil, nil, fmt.Errorf("writing shim: %w", err)
	}

	args := []string{"--headless", "-l", shimPath}
	if r.verbose {
		args = append([]string{"-V3"}, args...)
	}

	cmd := exec.CommandContext(ctx, r.nvimPath, args...)
	cmd.Env = append(os.Environ(), sb.Env()...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("nvim exited with error: %w\nstderr: %s", err, stderr.String())
	}

	return parseOutput(stdout.Bytes())
}

// parseOutput decodes the JSON emitted by the Lua harness.
func parseOutput(data []byte) (*domain.SuiteResult, *domain.CoverageData, error) {
	var out runOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, nil, fmt.Errorf("parsing harness output: %w\nraw: %s", err, string(data))
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
		fileCov := &domain.FileCoverage{
			Path:  fc.Path,
			Lines: make(map[int]int, len(fc.Lines)),
		}
		for lineStr, count := range fc.Lines {
			var lineNo int
			fmt.Sscan(lineStr, &lineNo)
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
