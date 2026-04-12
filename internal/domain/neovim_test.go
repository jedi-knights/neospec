package domain_test

import (
	"testing"

	"github.com/jedi-knights/neospec/internal/domain"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		wantTag string
		wantErr bool
	}{
		{input: "stable", wantTag: "stable"},
		{input: "nightly", wantTag: "nightly"},
		{input: "0.10.4", wantTag: "v0.10.4"},
		{input: "v0.10.4", wantTag: "v0.10.4"},
		{input: "1.0.0", wantTag: "v1.0.0"},
		{input: "garbage", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			v, err := domain.ParseVersion(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseVersion(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion(%q) unexpected error: %v", tc.input, err)
			}
			if v.Tag != tc.wantTag {
				t.Errorf("ParseVersion(%q).Tag = %q, want %q", tc.input, v.Tag, tc.wantTag)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"stable", "stable"},
		{"nightly", "nightly"},
		{"v0.10.4", "v0.10.4"},
		{"0.10.4", "v0.10.4"}, // ParseVersion adds "v" prefix
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			v, err := domain.ParseVersion(tc.input)
			if err != nil {
				t.Fatalf("ParseVersion(%q) error: %v", tc.input, err)
			}
			if got := v.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBinaryName(t *testing.T) {
	tests := []struct {
		platform domain.Platform
		want     string
	}{
		{domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}, "nvim"},
		{domain.Platform{OS: domain.OSDarwin, Arch: domain.ArchARM64}, "nvim"},
		{domain.Platform{OS: domain.OSWindows, Arch: domain.ArchAMD64}, "nvim.exe"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.platform.String(), func(t *testing.T) {
			if got := domain.BinaryName(tc.platform); got != tc.want {
				t.Errorf("BinaryName(%s) = %q, want %q", tc.platform, got, tc.want)
			}
		})
	}
}

func TestVersionAssetName(t *testing.T) {
	v, _ := domain.ParseVersion("stable")
	tests := []struct {
		platform domain.Platform
		want     string
	}{
		{domain.Platform{OS: domain.OSLinux, Arch: domain.ArchAMD64}, "nvim-linux-x86_64.tar.gz"},
		{domain.Platform{OS: domain.OSLinux, Arch: domain.ArchARM64}, "nvim-linux-arm64.tar.gz"},
		{domain.Platform{OS: domain.OSDarwin, Arch: domain.ArchAMD64}, "nvim-macos-x86_64.tar.gz"},
		{domain.Platform{OS: domain.OSDarwin, Arch: domain.ArchARM64}, "nvim-macos-x86_64.tar.gz"},
		{domain.Platform{OS: domain.OSWindows, Arch: domain.ArchAMD64}, "nvim-win64.zip"},
		{domain.Platform{OS: domain.OS("freebsd"), Arch: domain.ArchAMD64}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.platform.String(), func(t *testing.T) {
			if got := v.AssetName(tc.platform); got != tc.want {
				t.Errorf("AssetName(%s) = %q, want %q", tc.platform, got, tc.want)
			}
		})
	}
}
