//go:build !windows

package portal

import (
	"os/exec"
	"syscall"
)

// configureDetachedProcess sets process attributes for Unix to detach from terminal.
func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
