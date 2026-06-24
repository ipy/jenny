//go:build windows

package update

import (
	"os"
	"os/exec"

	"github.com/ipy/jenny/internal/constants"
)

const managedEnvKey = "_JENNY_MANAGED"

// IsManaged returns true if the current process was launched by the launcher.
func IsManaged() bool {
	return os.Getenv(managedEnvKey) == "1"
}

// TryLaunchManaged checks if a newer managed binary exists and execs into it.
// On Windows, syscall.Exec is not available so we spawn a child process and exit.
func TryLaunchManaged() {
	if IsManaged() {
		return
	}

	binPath := ManagedBinaryPath(constants.JennyHomeDir())
	if !ShouldLaunchManaged(binPath) {
		return
	}

	os.Setenv(managedEnvKey, "1")

	cmd := exec.Command(binPath, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Unsetenv(managedEnvKey)
		return
	}
	os.Exit(0)
}
