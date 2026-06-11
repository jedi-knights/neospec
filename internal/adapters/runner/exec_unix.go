//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// setProcessGroup makes the child the leader of a new process group (its PGID
// equals its PID) so the whole subtree can be signalled as a unit.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the child's entire process group, reaping
// any grandchildren (e.g. git clones spawned by lazy.nvim) the direct child
// left running. A negative PID targets the group whose ID equals the child PID,
// established via Setpgid in setProcessGroup. ESRCH (group already gone) is the
// normal, healthy case and is intentionally ignored.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
