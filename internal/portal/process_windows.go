//go:build windows

package portal

import (
	"os/exec"
	"syscall"
)

// configureDetachedProcess sets process attributes for Windows to hide the window.
func configureDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
