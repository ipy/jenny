// Package tool provides tool implementations.
package tool

import (
	"fmt"
	"runtime"
	"strings"
)

// WindowsCommandGate provides security validation for Windows commands.
type WindowsCommandGate struct {
	skipPermissions bool
}

// NewWindowsCommandGate creates a new WindowsCommandGate.
func NewWindowsCommandGate(skipPermissions bool) *WindowsCommandGate {
	return &WindowsCommandGate{skipPermissions: skipPermissions}
}

// CheckPath validates a Windows path against security restrictions.
// Returns an error if the path is blocked.
func (g *WindowsCommandGate) CheckPath(path string) error {
	if g.skipPermissions {
		return nil
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	// Normalize path for comparison (handle forward/back slashes)
	normPath := strings.ToLower(path)
	normPath = strings.ReplaceAll(normPath, "/", "\\")

	// Block C:\Windows\System32 and its subdirectories
	if strings.HasPrefix(normPath, "c:\\windows\\system32") {
		return fmt.Errorf("access to system directory %s is blocked", path)
	}

	// Block C:\Users\*\AppData and its subdirectories (roaming, local, localLow)
	if strings.Contains(normPath, "\\appdata") {
		return fmt.Errorf("access to AppData directory %s is blocked", path)
	}

	// Block C:\$Recycle.Bin and its subdirectories
	if strings.Contains(normPath, "$recycle.bin") {
		return fmt.Errorf("access to recycle bin %s is blocked", path)
	}

	// Block named pipes: \\.\pipe\...
	if strings.HasPrefix(normPath, "\\\\.\\pipe\\") {
		return fmt.Errorf("access to named pipe %s is blocked", path)
	}

	// Block raw physical drives: \\.\PhysicalDrive0, \\.\C:, etc.
	if strings.HasPrefix(normPath, "\\\\.\\") {
		// Check if it's a drive letter or physical drive reference
		if strings.HasPrefix(normPath, "\\\\.\\physicaldrive") {
			return fmt.Errorf("access to physical drive %s is blocked", path)
		}
		// Block single letter drive references like \\.\C:
		if len(normPath) >= 4 && normPath[4] == ':' {
			return fmt.Errorf("access to raw drive %s is blocked", path)
		}
	}

	return nil
}

// CheckCommand validates a Windows command against security restrictions.
// Returns an error if the command is blocked.
func (g *WindowsCommandGate) CheckCommand(command string) error {
	if g.skipPermissions {
		return nil
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	// Block Set-ExecutionPolicy commands (security policy modification)
	lowercaseCmd := strings.ToLower(command)
	if strings.Contains(lowercaseCmd, "set-executionpolicy") {
		return fmt.Errorf("modifying PowerShell execution policy is not allowed")
	}

	// Block reg.exe (Registry manipulation)
	tokens := strings.Fields(command)
	for _, token := range tokens {
		tokenLower := strings.ToLower(token)
		if tokenLower == "reg.exe" || tokenLower == "reg" {
			return fmt.Errorf("registry manipulation via reg.exe is not allowed")
		}
	}

	// Block sc.exe (Service Control Manager)
	for _, token := range tokens {
		tokenLower := strings.ToLower(token)
		if tokenLower == "sc.exe" || tokenLower == "sc" {
			return fmt.Errorf("service management via sc.exe is not allowed")
		}
	}

	return nil
}

// IsWindowsGateAvailable returns true on Windows platforms.
// This is used to conditionally use WindowsCommandGate on Windows.
func IsWindowsGateAvailable() bool {
	return runtime.GOOS == "windows"
}