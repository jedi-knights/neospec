package commands

import (
	"context"
	"os"
	"testing"

	"github.com/jedi-knights/neospec/internal/config"
	"github.com/jedi-knights/neospec/internal/domain"
)

func TestCheckThreshold_BelowThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 80.0}
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "f.lua", Lines: map[int]int{1: 1, 2: 0, 3: 0, 4: 0}}, // 25%
		},
	}
	err := checkThreshold(cfg, cov)
	if err == nil {
		t.Error("checkThreshold() expected error when below threshold")
	}
}

func TestCheckThreshold_AboveThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 50.0}
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "f.lua", Lines: map[int]int{1: 1, 2: 1}}, // 100%
		},
	}
	if err := checkThreshold(cfg, cov); err != nil {
		t.Errorf("checkThreshold() unexpected error: %v", err)
	}
}

func TestCheckThreshold_ZeroThreshold(t *testing.T) {
	cfg := config.Config{Threshold: 0}
	cov := &domain.CoverageData{} // 0% coverage
	if err := checkThreshold(cfg, cov); err != nil {
		t.Errorf("checkThreshold() with zero threshold should never error, got: %v", err)
	}
}

func TestApplyFlags_AllFlags(t *testing.T) {
	cfg := config.Config{}
	flags := &runFlags{
		neovimVersion: "nightly",
		patterns:      []string{"spec/**/*_spec.lua"},
		coverageDir:   "/tmp/cov",
		formats:       []string{"lcov", "junit"},
		threshold:     75.0,
		cacheDir:      "/tmp/cache",
		verbose:       true,
	}
	applyFlags(&cfg, flags)

	if cfg.NeovimVersion != "nightly" {
		t.Errorf("NeovimVersion = %q, want %q", cfg.NeovimVersion, "nightly")
	}
	if len(cfg.TestPatterns) != 1 || cfg.TestPatterns[0] != "spec/**/*_spec.lua" {
		t.Errorf("TestPatterns = %v", cfg.TestPatterns)
	}
	if cfg.CoverageDir != "/tmp/cov" {
		t.Errorf("CoverageDir = %q, want /tmp/cov", cfg.CoverageDir)
	}
	if len(cfg.Formats) != 2 {
		t.Errorf("Formats = %v", cfg.Formats)
	}
	if cfg.Threshold != 75.0 {
		t.Errorf("Threshold = %v, want 75.0", cfg.Threshold)
	}
	if cfg.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q, want /tmp/cache", cfg.CacheDir)
	}
	if !cfg.Verbose {
		t.Errorf("Verbose = false, want true")
	}
}

func TestApplyFlags_NoOverride(t *testing.T) {
	cfg := config.Config{
		NeovimVersion: "stable",
		CoverageDir:   "coverage",
	}
	flags := &runFlags{} // all zero values — nothing should be overridden
	applyFlags(&cfg, flags)

	if cfg.NeovimVersion != "stable" {
		t.Errorf("NeovimVersion should not be overridden, got %q", cfg.NeovimVersion)
	}
	if cfg.CoverageDir != "coverage" {
		t.Errorf("CoverageDir should not be overridden, got %q", cfg.CoverageDir)
	}
}

func TestReporterFor_Console(t *testing.T) {
	r, f, err := reporterFor("console", config.Config{}, false)
	if err != nil {
		t.Fatalf("reporterFor(console) error: %v", err)
	}
	if r == nil {
		t.Error("reporterFor(console) returned nil reporter")
	}
	if f != os.Stdout {
		t.Error("reporterFor(console) should return os.Stdout")
	}
}

func TestReporterFor_FileFormats(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{CoverageDir: dir}

	formats := []string{"lcov", "cobertura", "coveralls", "junit"}
	for _, format := range formats {
		format := format
		t.Run(format, func(t *testing.T) {
			r, f, err := reporterFor(format, cfg, false)
			if err != nil {
				t.Fatalf("reporterFor(%q) error: %v", format, err)
			}
			if r == nil {
				t.Errorf("reporterFor(%q) returned nil reporter", format)
			}
			if f == nil {
				t.Errorf("reporterFor(%q) returned nil file", format)
			}
			f.Close()
		})
	}
}

func TestReporterFor_Unknown(t *testing.T) {
	_, _, err := reporterFor("unknown-format", config.Config{}, false)
	if err == nil {
		t.Error("reporterFor(unknown) expected error")
	}
}

func TestEmitReports_Console(t *testing.T) {
	cfg := config.Config{Formats: []string{"console"}}
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{{Name: "test", Status: domain.StatusPass}},
	}
	cov := &domain.CoverageData{}

	if err := emitReports(context.Background(), cfg, suite, cov); err != nil {
		t.Errorf("emitReports() console error: %v", err)
	}
}

func TestEmitReports_FileFormat(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Formats:     []string{"lcov"},
		CoverageDir: dir,
	}
	suite := &domain.SuiteResult{}
	cov := &domain.CoverageData{}

	if err := emitReports(context.Background(), cfg, suite, cov); err != nil {
		t.Errorf("emitReports() lcov error: %v", err)
	}
}

func TestEmitReports_UnknownFormat(t *testing.T) {
	cfg := config.Config{Formats: []string{"unknown"}}
	suite := &domain.SuiteResult{}
	cov := &domain.CoverageData{}

	if err := emitReports(context.Background(), cfg, suite, cov); err == nil {
		t.Error("emitReports() expected error for unknown format")
	}
}

func TestReporterFor_FileFormats_CreateError(t *testing.T) {
	// A nonexistent directory causes os.Create to fail for all file-based formats.
	cfg := config.Config{CoverageDir: "/nonexistent/path/that/does/not/exist"}

	formats := []string{"lcov", "cobertura", "coveralls", "junit"}
	for _, format := range formats {
		format := format
		t.Run(format, func(t *testing.T) {
			_, _, err := reporterFor(format, cfg, false)
			if err == nil {
				t.Errorf("reporterFor(%q) expected error for nonexistent dir, got nil", format)
			}
		})
	}
}

func TestNewRunCmd(t *testing.T) {
	cmd := NewRunCmd()
	if cmd == nil {
		t.Fatal("NewRunCmd() returned nil")
	}
	if cmd.Use != "run" {
		t.Errorf("cmd.Use = %q, want %q", cmd.Use, "run")
	}
}
