// White-box tests for parsePlatform. CurrentPlatform delegates to parsePlatform,
// so testing parsePlatform directly avoids the need to change runtime.GOOS or
// runtime.GOARCH (compile-time constants that cannot be set from tests).
package domain

import "testing"

func TestParsePlatform(t *testing.T) {
	tests := []struct {
		goos     string
		goarch   string
		wantOS   OS
		wantArch Arch
		wantErr  bool
	}{
		{"linux", "amd64", OSLinux, ArchAMD64, false},
		{"linux", "arm64", OSLinux, ArchARM64, false},
		{"darwin", "amd64", OSDarwin, ArchAMD64, false},
		{"darwin", "arm64", OSDarwin, ArchARM64, false},
		{"windows", "amd64", OSWindows, ArchAMD64, false},
		// unsupported OS
		{"plan9", "amd64", "", "", true},
		{"freebsd", "amd64", "", "", true},
		// unsupported arch
		{"linux", "386", "", "", true},
		{"linux", "mips64", "", "", true},
		// both unsupported (OS check fires first)
		{"plan9", "mips64", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			p, err := parsePlatform(tc.goos, tc.goarch)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parsePlatform(%q, %q) expected error, got nil", tc.goos, tc.goarch)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePlatform(%q, %q) unexpected error: %v", tc.goos, tc.goarch, err)
			}
			if p.OS != tc.wantOS {
				t.Errorf("OS = %q, want %q", p.OS, tc.wantOS)
			}
			if p.Arch != tc.wantArch {
				t.Errorf("Arch = %q, want %q", p.Arch, tc.wantArch)
			}
		})
	}
}
