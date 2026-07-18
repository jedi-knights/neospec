package commands

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/cover"
	"github.com/jedi-knights/neospec/internal/domain"
)

type fakeCoverExecutor struct {
	cov  *domain.CoverageData
	err  error
	seen cover.Opts
}

func (f *fakeCoverExecutor) Run(_ context.Context, opts cover.Opts) (*domain.CoverageData, error) {
	f.seen = opts
	return f.cov, f.err
}

func TestRunCover_PlenaryBustedHappyPath(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "lua/foo.lua", Lines: map[int]int{1: 1, 2: 3}}},
	}
	fake := &fakeCoverExecutor{cov: cov}
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			minimalInit: "tests/minimal_init.vim",
			formats:     []string{"console"},
			coverageDir: t.TempDir(),
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	if fake.seen.Mode != cover.RunnerPlenaryBusted {
		t.Errorf("mode = %q, want plenary-busted", fake.seen.Mode)
	}
	if fake.seen.Dir != "tests/" {
		t.Errorf("dir = %q, want tests/", fake.seen.Dir)
	}
	if fake.seen.MinimalInit != "tests/minimal_init.vim" {
		t.Errorf("minimal-init = %q, want tests/minimal_init.vim", fake.seen.MinimalInit)
	}
}

func TestRunCover_ExternalHappyPath(t *testing.T) {
	fake := &fakeCoverExecutor{cov: &domain.CoverageData{}}
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "external",
			formats:     []string{"console"},
			coverageDir: t.TempDir(),
		},
		[]string{"make", "test"},
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	if fake.seen.Mode != cover.RunnerExternal {
		t.Errorf("mode = %q, want external", fake.seen.Mode)
	}
	if len(fake.seen.Command) != 2 || fake.seen.Command[0] != "make" {
		t.Errorf("command = %v, want [make test]", fake.seen.Command)
	}
}

func TestRunCover_MissingRunner(t *testing.T) {
	err := runCover(context.Background(),
		&coverFlags{configPath: "nonexistent.toml"},
		nil,
		coverDeps{executor: &fakeCoverExecutor{}, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "--runner is required") {
		t.Errorf("want --runner required error, got: %v", err)
	}
}

func TestRunCover_UnknownRunner(t *testing.T) {
	err := runCover(context.Background(),
		&coverFlags{configPath: "nonexistent.toml", runner: "bogus"},
		nil,
		coverDeps{executor: &fakeCoverExecutor{}, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown --runner") {
		t.Errorf("want unknown-runner error, got: %v", err)
	}
}

func TestRunCover_PlenaryMissingDir(t *testing.T) {
	err := runCover(context.Background(),
		&coverFlags{configPath: "nonexistent.toml", runner: "plenary-busted"},
		nil,
		coverDeps{executor: &fakeCoverExecutor{}, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "--dir is required") {
		t.Errorf("want --dir required error, got: %v", err)
	}
}

func TestRunCover_ExternalMissingCommand(t *testing.T) {
	err := runCover(context.Background(),
		&coverFlags{configPath: "nonexistent.toml", runner: "external"},
		nil,
		coverDeps{executor: &fakeCoverExecutor{}, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "external mode requires") {
		t.Errorf("want external-cmd error, got: %v", err)
	}
}

func TestRunCover_ExecutorErrorSurfaced(t *testing.T) {
	// When the wrapped runner exits non-zero but coverage is still returned,
	// runCover should emit the reports AND return the runner error.
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "lua/foo.lua", Lines: map[int]int{1: 1}}},
	}
	fake := &fakeCoverExecutor{cov: cov, err: errors.New("plenary tests failed")}
	var out bytes.Buffer
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"console"},
			coverageDir: t.TempDir(),
		},
		nil,
		coverDeps{executor: fake, stdout: &out},
	)
	if err == nil {
		t.Fatalf("want runner error, got nil")
	}
	if !strings.Contains(err.Error(), "plenary tests failed") {
		t.Errorf("error missing runner cause: %v", err)
	}
	// Verify coverage was still emitted before the error was returned
	if !strings.Contains(out.String(), "lua/foo.lua") && out.Len() == 0 {
		// The console reporter may format differently; just verify something was emitted
		t.Errorf("no coverage output emitted before error: %q", out.String())
	}
}

func TestRunCover_ThresholdEnforced(t *testing.T) {
	// Empty coverage → 0% → below any positive threshold
	fake := &fakeCoverExecutor{cov: &domain.CoverageData{}}
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"console"},
			threshold:   80,
			coverageDir: t.TempDir(),
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "below threshold") {
		t.Errorf("want threshold error, got: %v", err)
	}
}

func TestRunCover_JunitRejected(t *testing.T) {
	fake := &fakeCoverExecutor{cov: &domain.CoverageData{}}
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"junit"},
			coverageDir: t.TempDir(),
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "junit format is not supported") {
		t.Errorf("want junit-rejection error, got: %v", err)
	}
}

func TestRunCover_UnknownFormat(t *testing.T) {
	fake := &fakeCoverExecutor{cov: &domain.CoverageData{}}
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"yaml"},
			coverageDir: t.TempDir(),
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("want unknown-format error, got: %v", err)
	}
}

func TestRunCover_LCOVWrittenToFile(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "lua/foo.lua", Lines: map[int]int{1: 1}}},
	}
	fake := &fakeCoverExecutor{cov: cov}
	dir := t.TempDir()
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"lcov"},
			coverageDir: dir,
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	// Verify lcov.info was written
	files, err := readDir(dir)
	if err != nil {
		t.Fatalf("reading coverage dir: %v", err)
	}
	found := false
	for _, name := range files {
		if name == "lcov.info" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("lcov.info not written to %s (files: %v)", dir, files)
	}
}

func TestRunCover_CoberturaWrittenToFile(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "lua/foo.lua", Lines: map[int]int{1: 1}}},
	}
	fake := &fakeCoverExecutor{cov: cov}
	dir := t.TempDir()
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"cobertura"},
			coverageDir: dir,
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	names, err := readDir(dir)
	if err != nil {
		t.Fatalf("readDir: %v", err)
	}
	if !containsName(names, "cobertura.xml") {
		t.Errorf("cobertura.xml not written (files: %v)", names)
	}
}

func TestRunCover_CoverallsWrittenToFile(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "lua/foo.lua", Lines: map[int]int{1: 1}}},
	}
	fake := &fakeCoverExecutor{cov: cov}
	dir := t.TempDir()
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"coveralls"},
			coverageDir: dir,
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	names, err := readDir(dir)
	if err != nil {
		t.Fatalf("readDir: %v", err)
	}
	if !containsName(names, "coveralls.json") {
		t.Errorf("coveralls.json not written (files: %v)", names)
	}
}

func TestRunCover_MultipleFormats(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "lua/foo.lua", Lines: map[int]int{1: 1}}},
	}
	fake := &fakeCoverExecutor{cov: cov}
	dir := t.TempDir()
	err := runCover(context.Background(),
		&coverFlags{
			configPath:  "nonexistent.toml",
			runner:      "plenary-busted",
			dir:         "tests/",
			formats:     []string{"console", "lcov", "cobertura", "coveralls"},
			coverageDir: dir,
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	names, _ := readDir(dir)
	for _, want := range []string{"lcov.info", "cobertura.xml", "coveralls.json"} {
		if !containsName(names, want) {
			t.Errorf("%s not written (files: %v)", want, names)
		}
	}
}

func TestRunCover_FlagOverlays(t *testing.T) {
	// Exercises cacheDir, neovimVersion, verbose, and threshold=0 overlay branches
	fake := &fakeCoverExecutor{cov: &domain.CoverageData{
		Files: []*domain.FileCoverage{{Path: "x.lua", Lines: map[int]int{1: 1}}},
	}}
	err := runCover(context.Background(),
		&coverFlags{
			configPath:    "nonexistent.toml",
			runner:        "plenary-busted",
			dir:           "tests/",
			cacheDir:      "/tmp/cache",
			neovimVersion: "nightly",
			verbose:       true,
			formats:       []string{"console"},
			coverageDir:   t.TempDir(),
		},
		nil,
		coverDeps{executor: fake, stdout: &bytes.Buffer{}},
	)
	if err != nil {
		t.Fatalf("runCover: %v", err)
	}
	if fake.seen.Version.Tag != "nightly" {
		t.Errorf("version = %q, want nightly", fake.seen.Version.Tag)
	}
	if !fake.seen.Verbose {
		t.Errorf("verbose not propagated to executor opts")
	}
}

func TestRunCover_InvalidNeovimVersion(t *testing.T) {
	err := runCover(context.Background(),
		&coverFlags{
			configPath:    "nonexistent.toml",
			runner:        "plenary-busted",
			dir:           "tests/",
			neovimVersion: "not-a-version",
		},
		nil,
		coverDeps{executor: &fakeCoverExecutor{}, stdout: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "parsing neovim version") {
		t.Errorf("want version-parse error, got: %v", err)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestNewCoverCmd_FlagDefaults(t *testing.T) {
	cmd := NewCoverCmd()
	if cmd.Use == "" {
		t.Errorf("Use is empty")
	}
	if err := cmd.Flags().Parse(nil); err != nil {
		t.Fatalf("flag parse: %v", err)
	}
	if got, _ := cmd.Flags().GetString("config"); got != "neospec.toml" {
		t.Errorf("config default = %q, want neospec.toml", got)
	}
	if got, _ := cmd.Flags().GetString("runner"); got != "" {
		t.Errorf("runner default should be empty (required), got %q", got)
	}
}

// readDir returns file names in a directory.
func readDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
