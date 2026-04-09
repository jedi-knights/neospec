// Package badge implements ports.BadgePatcher. It finds the first shields.io
// coverage badge in a README file and replaces its URL with the one computed
// from the current coverage percentage.
package badge

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/jedi-knights/neospec/internal/domain"
)

// shieldsBadgeRe matches any shields.io badge URL for a "coverage" label.
// It captures the full URL so it can be replaced in-place.
//
// Matches examples:
//   - https://img.shields.io/badge/coverage-87.5%25-brightgreen
//   - https://img.shields.io/badge/coverage-0.0%25-red
var shieldsBadgeRe = regexp.MustCompile(
	`https://img\.shields\.io/badge/coverage-[^-]+-\w+`,
)

// Patcher implements ports.BadgePatcher.
type Patcher struct{}

// NewPatcher creates a Patcher.
func NewPatcher() *Patcher { return &Patcher{} }

// Patch reads readmePath, replaces the first matching coverage badge URL with
// the URL for pct, and writes the file back. If no badge is found, the file is
// left unchanged.
func (p *Patcher) Patch(_ context.Context, readmePath string, pct float64) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("reading readme %s: %w", readmePath, err)
	}

	newURL := domain.ShieldsBadgeURL(pct)
	updated := shieldsBadgeRe.ReplaceAll(data, []byte(newURL))

	if string(updated) == string(data) {
		// No badge found — nothing to do.
		return nil
	}

	if err := os.WriteFile(readmePath, updated, 0o644); err != nil {
		return fmt.Errorf("writing readme %s: %w", readmePath, err)
	}
	return nil
}
