package utils

import "strings"

// IsUserAbortError checks if an error message indicates a user-initiated abort.
// User aborts include context cancellation, Esc key, SIGINT, etc.
func IsUserAbortError(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	lowerMsg := strings.ToLower(errMsg)
	if strings.Contains(lowerMsg, "context canceled") ||
		strings.Contains(lowerMsg, "context cancelled") ||
		strings.Contains(lowerMsg, "context deadline exceeded") {
		return true
	}
	if strings.Contains(lowerMsg, "user interrupt") ||
		strings.Contains(lowerMsg, "sigint") ||
		strings.Contains(lowerMsg, "keyboard interrupt") {
		return true
	}
	return false
}
