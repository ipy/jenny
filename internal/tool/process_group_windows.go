//go:build windows

package tool

import (
	"os"
	"os/exec"
	"strconv"
)

// setupProcessGroup is a no-op on Windows. The Windows process model has no
// direct equivalent of Unix process groups; tree termination is handled via
// taskkill /F /T (see killProcessGroup and signal_windows.go).
func setupProcessGroup(cmd *exec.Cmd) {
	// Intentionally empty: Windows job objects and taskkill cover the same
	// need; setting SysProcAttr fields here would break cross-compilation.
}

// killProcessGroup terminates the process and all of its descendants on
// Windows by shelling out to taskkill /F /T. This mirrors the behavior of
// the Unix process-group SIGKILL.
func killProcessGroup(proc *os.Process) error {
	if proc == nil {
		return nil
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(proc.Pid))
	return cmd.Run()
}
