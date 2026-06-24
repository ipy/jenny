//go:build !windows

package update

import (
	"os"
	"syscall"

	"github.com/ipy/jenny/internal/constants"
)

const managedEnvKey = "_JENNY_MANAGED"

// IsManaged returns true if the current process was launched by the launcher.
func IsManaged() bool {
	return os.Getenv(managedEnvKey) == "1"
}

// TryLaunchManaged checks if a newer managed binary exists and execs into it.
// This should be called very early in main(), before any other initialization.
// Returns only if no managed binary is available (normal execution continues).
// If exec succeeds, this function never returns.
func TryLaunchManaged() {
	if IsManaged() {
		return
	}

	binPath := ManagedBinaryPath(constants.JennyHomeDir())
	if !ShouldLaunchManaged(binPath) {
		return
	}

	os.Setenv(managedEnvKey, "1")

	err := syscall.Exec(binPath, os.Args, os.Environ())
	if err != nil {
		os.Unsetenv(managedEnvKey)
	}
}
