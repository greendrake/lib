//go:build unix

package aws

import (
	"os/exec"
	"syscall"
)

// detach starts a command in a process group of its own, out of the reach of
// a terminal's interrupt.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// stop ends a detached command's process group.
func stop(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
