// Package domain contains the core data types and pure business logic for neospec.
// Nothing in this package performs I/O or depends on external packages — it is
// the stable heart of the application that all other packages point toward.
package domain

import (
	"fmt"
	"runtime"
)

// OS represents a target operating system.
type OS string

// Supported operating systems.
const (
	OSLinux   OS = "linux"
	OSDarwin  OS = "darwin"
	OSWindows OS = "windows"
)

// Arch represents a CPU architecture.
type Arch string

// Supported CPU architectures.
const (
	ArchAMD64 Arch = "x86_64"
	ArchARM64 Arch = "arm64"
)

// Platform combines an OS and architecture for use in Neovim release asset selection.
type Platform struct {
	OS   OS
	Arch Arch
}

// CurrentPlatform returns the Platform for the running process.
func CurrentPlatform() (Platform, error) {
	return parsePlatform(runtime.GOOS, runtime.GOARCH)
}

// parsePlatform maps a GOOS/GOARCH pair to a Platform. It is the testable
// inner function for CurrentPlatform — callers that need to simulate unusual
// or unsupported platforms can call it directly in white-box tests.
func parsePlatform(goos, goarch string) (Platform, error) {
	var os OS
	switch goos {
	case "linux":
		os = OSLinux
	case "darwin":
		os = OSDarwin
	case "windows":
		os = OSWindows
	default:
		return Platform{}, fmt.Errorf("unsupported OS: %s", goos)
	}

	var arch Arch
	switch goarch {
	case "amd64":
		arch = ArchAMD64
	case "arm64":
		arch = ArchARM64
	default:
		return Platform{}, fmt.Errorf("unsupported architecture: %s", goarch)
	}

	return Platform{OS: os, Arch: arch}, nil
}

// String returns a human-readable platform string, e.g. "linux/x86_64".
func (p Platform) String() string {
	return fmt.Sprintf("%s/%s", p.OS, p.Arch)
}
