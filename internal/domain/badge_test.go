package domain_test

import (
	"fmt"
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestBadgeColor(t *testing.T) {
	tests := []struct {
		pct   float64
		color string
	}{
		{100, "brightgreen"},
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
		t.Run(fmt.Sprintf("pct=%.0f", tc.pct), func(t *testing.T) {
			if got := domain.BadgeColor(tc.pct); got != tc.color {
				t.Errorf("BadgeColor(%.0f) = %q, want %q", tc.pct, got, tc.color)
			}
		})
	}
}

func TestBadgeLabel(t *testing.T) {
	tests := []struct {
		pct  float64
		want string
	}{
		{0, "0.0%"},
		{87.5, "87.5%"},
		{100, "100.0%"},
		{33.333, "33.3%"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := domain.BadgeLabel(tc.pct); got != tc.want {
				t.Errorf("BadgeLabel(%v) = %q, want %q", tc.pct, got, tc.want)
			}
		})
	}
}
