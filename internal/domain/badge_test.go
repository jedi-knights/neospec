package domain_test

import (
	"strings"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestBadgeColor(t *testing.T) {
	tests := []struct {
		pct   float64
		color string
	}{
		{95, "brightgreen"},
		{90, "brightgreen"},
		{89, "green"},
		{75, "green"},
		{74, "yellow"},
		{60, "yellow"},
		{59, "orange"},
		{40, "orange"},
		{39, "red"},
		{0, "red"},
	}
	for _, tc := range tests {
		if got := domain.BadgeColor(tc.pct); got != tc.color {
			t.Errorf("BadgeColor(%.0f) = %q, want %q", tc.pct, got, tc.color)
		}
	}
}

func TestShieldsBadgeURL(t *testing.T) {
	url := domain.ShieldsBadgeURL(87.5)
	if !strings.HasPrefix(url, "https://img.shields.io/badge/coverage-") {
		t.Errorf("unexpected badge URL: %s", url)
	}
	if !strings.Contains(url, "green") {
		t.Errorf("badge URL missing color: %s", url)
	}
}
