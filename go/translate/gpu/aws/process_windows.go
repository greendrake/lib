package aws

import (
	"os/exec"
	"syscall"
)

// detach starts a command in a process group of its own, out of the reach of
// a console's interrupt.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// stop ends a detached command.
func stop(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
