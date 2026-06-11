// Package tool provides tool implementations.
//go:build !windows

package tool

import (
	"os"
	"syscall"
)

// signalProcess sends a signal to a process.
// On non-Windows systems, this sends SIGTERM.
func signalProcess(proc *os.Process, isWindows bool) error {
	return proc.Signal(syscall.SIGTERM)
}

// escalateProcessKill escalates a process termination with SIGKILL.
// On non-Windows systems, this sends SIGKILL.
func escalateProcessKill(proc *os.Process, isWindows bool) error {
	return proc.Signal(syscall.SIGKILL)
}