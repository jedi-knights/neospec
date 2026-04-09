package domain_test

import (
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestCurrentPlatform(t *testing.T) {
	p, err := domain.CurrentPlatform()
	if err != nil {
		t.Fatalf("CurrentPlatform() unexpected error: %v", err)
	}
	if p.OS == "" {
		t.Error("platform OS is empty")
	}
	if p.Arch == "" {
		t.Error("platform Arch is empty")
	}
}

func TestPlatformString(t *testing.T) {
	p := domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}
	if got := p.String(); got != "linux/x86_64" {
		t.Errorf("Platform.String() = %q, want %q", got, "linux/x86_64")
	}
}
