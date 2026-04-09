package domain

import "fmt"

// BadgeColor returns the shields.io color name for a given coverage percentage.
// The thresholds mirror common open-source conventions.
func BadgeColor(pct float64) string {
	switch {
	case pct >= 90:
		return "brightgreen"
	case pct >= 75:
		return "green"
	case pct >= 60:
		return "yellow"
	case pct >= 40:
		return "orange"
	default:
		return "red"
	}
}

// BadgeLabel returns the display label for a coverage percentage, e.g. "87.5%".
func BadgeLabel(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}

// ShieldsBadgeURL returns a shields.io badge URL for the given coverage percentage.
// The URL is stable (same percentage always produces the same URL) so it can be
// written into a README and committed.
func ShieldsBadgeURL(pct float64) string {
	label := BadgeLabel(pct)
	color := BadgeColor(pct)
	return fmt.Sprintf("https://img.shields.io/badge/coverage-%s-%s", label, color)
}
