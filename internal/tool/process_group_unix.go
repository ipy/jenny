//go:build !windows

package tool

import (
	"os"
	"os/exec"
	"syscall"
)

// setupProcessGroup configures the command to run in its own Unix process group
// so the entire tree (shell + grandchildren) can be killed as a unit.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the entire process group rooted at the
// given leader process. Caller must ensure setupProcessGroup was used so the
// PID is a process-group leader.
func killProcessGroup(proc *os.Process) error {
	return syscall.Kill(-proc.Pid, syscall.SIGKILL)
}
