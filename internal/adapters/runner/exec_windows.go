//go:build windows

package runner

import "os/exec"

// setProcessGroup is a no-op on Windows, which has no POSIX process groups.
// Reaping the full descendant tree would require a Job Object; neospec's CI
// targets Unix, so the Unix implementation carries the real fix.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup best-effort terminates the direct child on Windows.
// Grandchild reaping is not implemented here.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
