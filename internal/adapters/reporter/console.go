// Package reporter contains implementations of ports.Reporter for each
// supported output format.
package reporter

import (
	"context"
	"fmt"
	"io"

	"github.com/jedi-knights/neospec/internal/domain"
)

// Console writes a human-readable test and coverage summary to the writer.
// It uses ANSI color codes when the writer is likely a TTY; callers that need
// plain text can wrap the writer with a color-stripping adapter.
type Console struct {
	// Color controls whether ANSI escape codes are emitted.
	Color bool
}

// NewConsole creates a Console reporter. Pass color=true for terminal output.
func NewConsole(color bool) *Console {
	return &Console{Color: color}
}

func (c *Console) Write(_ context.Context, w io.Writer, suite *domain.SuiteResult, cov *domain.CoverageData) error {
	pass, fail, skip, errors := suite.Counts()

	for _, t := range suite.Tests {
		symbol, color := c.statusSymbol(t.Status)
		c.fprintColor(w, color, fmt.Sprintf("  %s %s\n", symbol, t.Name))
		if t.Error != "" {
			c.fprintColor(w, colorRed, fmt.Sprintf("    %s\n", t.Error))
		}
	}

	fmt.Fprintln(w)
	summary := fmt.Sprintf(
		"Tests: %d passed, %d failed, %d skipped, %d errors  (%.2fs)\n",
		pass, fail, skip, errors,
		suite.Duration.Seconds(),
	)
	if fail > 0 || errors > 0 {
		c.fprintColor(w, colorRed, summary)
	} else {
		c.fprintColor(w, colorGreen, summary)
	}

	if cov != nil && cov.TotalLines() > 0 {
		pct := cov.Percentage()
		covLine := fmt.Sprintf("Coverage: %s (%d/%d lines)\n",
			domain.BadgeLabel(pct), cov.HitLines(), cov.TotalLines())
		c.fprintColor(w, colorForPct(pct), covLine)
	}

	return nil
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorOrange = "\033[38;5;208m"
)

func colorForPct(pct float64) string {
	switch domain.BadgeColor(pct) {
	case "brightgreen", "green":
		return colorGreen
	case "yellow":
		return colorYellow
	case "orange":
		return colorOrange
	default:
		return colorRed
	}
}

func (c *Console) fprintColor(w io.Writer, color, s string) {
	if c.Color {
		fmt.Fprintf(w, "%s%s%s", color, s, colorReset)
	} else {
		fmt.Fprint(w, s)
	}
}

func (c *Console) statusSymbol(s domain.TestStatus) (string, string) {
	switch s {
	case domain.StatusPass:
		return "✓", colorGreen
	case domain.StatusFail:
		return "✗", colorRed
	case domain.StatusSkip:
		return "○", colorYellow
	default:
		return "!", colorOrange
	}
}
