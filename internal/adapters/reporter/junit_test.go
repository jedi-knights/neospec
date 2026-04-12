package reporter_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jedi-knights/neospec/internal/adapters/reporter"
	"github.com/jedi-knights/neospec/internal/domain"
)

// failingWriter is an io.Writer that always returns an error.
// Used to exercise XML/JSON encoding error branches across reporter tests.
type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("simulated write failure")
}

func TestJUnit_Write_AllStatuses(t *testing.T) {
	suite := &domain.SuiteResult{
		Tests: []domain.TestResult{
			{Name: "passing test", Status: domain.StatusPass, Duration: 10 * time.Millisecond},
			{Name: "failing test", Status: domain.StatusFail, Error: "assertion failed", Duration: 5 * time.Millisecond},
			{Name: "skipped test", Status: domain.StatusSkip},
			{Name: "error test", Status: domain.StatusError, Error: "runtime error"},
		},
		Duration: 50 * time.Millisecond,
	}
	var buf bytes.Buffer
	r := reporter.NewJUnit()
	if err := r.Write(context.Background(), &buf, suite, nil); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("missing XML declaration:\n%s", got)
	}
	if !strings.Contains(got, "passing test") {
		t.Errorf("missing passing test name:\n%s", got)
	}
	if !strings.Contains(got, "failing test") {
		t.Errorf("missing failing test name:\n%s", got)
	}
	if !strings.Contains(got, "assertion failed") {
		t.Errorf("missing failure message:\n%s", got)
	}
	if !strings.Contains(got, "<failure") {
		t.Errorf("missing <failure> element:\n%s", got)
	}
	if !strings.Contains(got, "<error") {
		t.Errorf("missing <error> element:\n%s", got)
	}
	if !strings.Contains(got, "<skipped") {
		t.Errorf("missing <skipped> element:\n%s", got)
	}
	// Counts at suite level
	if !strings.Contains(got, `failures="1"`) {
		t.Errorf("expected failures=1:\n%s", got)
	}
	if !strings.Contains(got, `errors="1"`) {
		t.Errorf("expected errors=1:\n%s", got)
	}
	if !strings.Contains(got, `skipped="1"`) {
		t.Errorf("expected skipped=1:\n%s", got)
	}
}

// TestJUnit_Write_EncodeError covers the xml.Encoder.Encode error branch.
// A failing writer causes enc.Encode to return an error.
func TestJUnit_Write_EncodeError(t *testing.T) {
	r := reporter.NewJUnit()
	err := r.Write(context.Background(), failingWriter{}, &domain.SuiteResult{}, nil)
	if err == nil {
		t.Fatal("Write() expected error on encode failure, got nil")
	}
}

func TestJUnit_Write_EmptySuite(t *testing.T) {
	suite := &domain.SuiteResult{}
	var buf bytes.Buffer
	r := reporter.NewJUnit()
	if err := r.Write(context.Background(), &buf, suite, nil); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "<testsuites") {
		t.Errorf("missing <testsuites>:\n%s", got)
	}
	if !strings.Contains(got, `tests="0"`) {
		t.Errorf("expected tests=0:\n%s", got)
	}
}
