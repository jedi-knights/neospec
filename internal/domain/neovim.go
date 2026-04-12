package domain

import (
	"fmt"
	"regexp"
	"strconv"
)

// Version represents a Neovim release version. The Tag field is the canonical
// form used in GitHub release URLs (e.g. "v0.10.4", "stable", "nightly").
type Version struct {
	Major int
	Minor int
	Patch int
	// Tag is the GitHub release tag, e.g. "stable", "nightly", or "v0.10.4".
	Tag string
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// ParseVersion parses a version string. It accepts:
//   - "stable" and "nightly" as special tags with zero Major/Minor/Patch
//   - semantic version strings like "0.10.4" or "v0.10.4"
func ParseVersion(s string) (Version, error) {
	if s == "stable" || s == "nightly" {
		return Version{Tag: s}, nil
	}

	m := semverRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("invalid neovim version %q: want \"stable\", \"nightly\", or semver like \"0.10.4\"", s)
	}

	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])

	tag := s
	if s[0] != 'v' {
		tag = "v" + s
	}

	return Version{Major: major, Minor: minor, Patch: patch, Tag: tag}, nil
}

// String returns the canonical version tag.
func (v Version) String() string {
	return v.Tag
}

// AssetName returns the filename of the Neovim release archive for a given platform.
// These match the actual asset names used in Neovim GitHub releases.
func (v Version) AssetName(p Platform) string {
	switch p.OS {
	case OSDarwin:
		// As of Neovim 0.10, GitHub releases ship separate x86_64 and arm64
		// tarballs for macOS rather than a single universal binary.
		if p.Arch == ArchARM64 {
			return "nvim-macos-arm64.tar.gz"
		}
		return "nvim-macos-x86_64.tar.gz"
	case OSLinux:
		switch p.Arch {
		case ArchARM64:
			return "nvim-linux-arm64.tar.gz"
		default:
			return "nvim-linux-x86_64.tar.gz"
		}
	case OSWindows:
		return "nvim-win64.zip"
	}
	return ""
}

// BinaryName returns the Neovim executable name for a platform.
func BinaryName(p Platform) string {
	if p.OS == OSWindows {
		return "nvim.exe"
	}
	return "nvim"
}
