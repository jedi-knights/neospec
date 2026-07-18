package domain

import (
	"fmt"
	"time"
)

// MatrixResult captures the outcome of running a single command under a single
// Neovim version. It is emitted by the exec-matrix adapter and consumed by the
// exec command's reporter.
type MatrixResult struct {
	// Version is the Neovim release the command ran against.
	Version Version
	// ExitCode is the command's process exit code. Zero on success.
	ExitCode int
	// Stdout is the command's captured standard output.
	Stdout []byte
	// Stderr is the command's captured standard error.
	Stderr []byte
	// Duration is the wall-clock time the command took to run under this version.
	Duration time.Duration
	// Err is a non-nil provisioning or process error that prevented the command
	// from producing a meaningful ExitCode (e.g. the Neovim binary could not be
	// downloaded, or the sandbox could not be created). When Err is non-nil,
	// ExitCode should be treated as unset.
	Err error
}

// Passed reports whether the command ran to completion under this version with
// a zero exit code and no provisioning error.
func (r MatrixResult) Passed() bool {
	return r.Err == nil && r.ExitCode == 0
}

// ExecMatrix aggregates the per-version results of a single exec-matrix run.
type ExecMatrix struct {
	// Command is the argv-shape command that was executed under each version.
	// The first element is the executable name; the rest are arguments.
	Command []string
	// Results is one entry per version, in the order the versions were requested.
	Results []MatrixResult
	// Duration is the total wall-clock time across every version's run.
	Duration time.Duration
}

// PassedCount returns the number of versions whose command ran to completion
// with a zero exit code.
func (m ExecMatrix) PassedCount() int {
	n := 0
	for _, r := range m.Results {
		if r.Passed() {
			n++
		}
	}
	return n
}

// FailedCount returns the number of versions whose command failed — either
// because the process exited non-zero or because provisioning errored before
// the command could run.
func (m ExecMatrix) FailedCount() int {
	return len(m.Results) - m.PassedCount()
}

// Passed reports whether every requested version passed. An empty matrix
// (no versions requested) is considered failed to prevent silent no-ops from
// being interpreted as success by CI wrappers.
func (m ExecMatrix) Passed() bool {
	if len(m.Results) == 0 {
		return false
	}
	return m.FailedCount() == 0
}

// FailedVersions returns the tag strings of every version whose result failed,
// in the order they were requested. Useful for a one-line summary line that
// names which versions to investigate.
func (m ExecMatrix) FailedVersions() []string {
	var tags []string
	for _, r := range m.Results {
		if !r.Passed() {
			tags = append(tags, r.Version.Tag)
		}
	}
	return tags
}

// String returns a one-line human-readable summary of the matrix outcome.
func (m ExecMatrix) String() string {
	return fmt.Sprintf("%d/%d versions passed in %s",
		m.PassedCount(), len(m.Results), m.Duration.Round(time.Millisecond))
}
