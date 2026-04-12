package reporter_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/domain"
)

func TestCobertura_Write_NilCov(t *testing.T) {
	var buf bytes.Buffer
	r := reporter.NewCobertura()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, nil); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("missing XML declaration:\n%s", got)
	}
	if strings.Contains(got, "DOCTYPE") {
		t.Errorf("output must not contain DOCTYPE:\n%s", got)
	}
	if !strings.Contains(got, `<coverage`) {
		t.Errorf("missing <coverage> element:\n%s", got)
	}
	// nil cov → 0 lines
	if !strings.Contains(got, `lines-valid="0"`) {
		t.Errorf("expected lines-valid=0:\n%s", got)
	}
}

func TestCobertura_Write_WithCoverage(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/init.lua", Lines: map[int]int{1: 3, 2: 0, 3: 1}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewCobertura()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "lua/init.lua") {
		t.Errorf("missing file path:\n%s", got)
	}
	// 3 total lines, 2 hit → lines-valid=3 lines-covered=2
	if !strings.Contains(got, `lines-valid="3"`) {
		t.Errorf("expected lines-valid=3:\n%s", got)
	}
	if !strings.Contains(got, `lines-covered="2"`) {
		t.Errorf("expected lines-covered=2:\n%s", got)
	}
	// line numbers in output
	if !strings.Contains(got, `number="1"`) {
		t.Errorf("expected line number 1:\n%s", got)
	}
}

func TestCobertura_Write_MultipleFiles(t *testing.T) {
	cov := &domain.CoverageData{
		Files: []*domain.FileCoverage{
			{Path: "lua/a.lua", Lines: map[int]int{1: 1, 2: 0}},
			{Path: "lua/b.lua", Lines: map[int]int{1: 1, 2: 1}},
		},
	}
	var buf bytes.Buffer
	r := reporter.NewCobertura()
	if err := r.Write(context.Background(), &buf, &domain.SuiteResult{}, cov); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "lua/a.lua") {
		t.Errorf("missing lua/a.lua:\n%s", got)
	}
	if !strings.Contains(got, "lua/b.lua") {
		t.Errorf("missing lua/b.lua:\n%s", got)
	}
}
