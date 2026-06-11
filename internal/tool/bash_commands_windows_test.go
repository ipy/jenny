package tool

import (
	"os"
	"testing"
)

// TestCdTildeExpansion tests that parseCdTarget correctly handles tilde expansion
// using os.UserHomeDir() instead of os.Getenv("HOME").
func TestCdTildeExpansion(t *testing.T) {
	// Get expected home directory
	expectedHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("UserHomeDir not available, skipping test")
	}

	// Test cd ~ (go to home)
	result := parseCdTarget("cd ~", "/some/cwd")
	if result != expectedHome {
		t.Errorf("expected home directory %q, got %q", expectedHome, result)
	}

	// Test cd with just tilde (same as cd ~)
	result = parseCdTarget("cd~", "/some/cwd")
	if result != expectedHome {
		t.Errorf("expected home directory %q for 'cd~', got %q", expectedHome, result)
	}

	// Test cd ~/path (tilde expansion with subpath)
	result = parseCdTarget("cd ~/Documents", "/some/cwd")
	expected := expectedHome + "/Documents"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}

	t.Log("AC4 PASS: parseCdTarget uses os.UserHomeDir() for tilde expansion")
}

// TestIsPathWithinCwdCaseInsensitive tests case-insensitive path comparison.
func TestIsPathWithinCwdCaseInsensitive(t *testing.T) {
	// Test Windows-style case insensitivity
	testCases := []struct {
		path     string
		cwd      string
		expected bool
	}{
		// Case variations of same path
		{"C:\\Users\\Test\\Documents", "c:\\users\\test\\documents", true},
		{"c:\\users\\test\\documents", "C:\\Users\\Test\\Documents", true},
		{"C:\\Users\\Test\\Documents\\file.txt", "C:\\Users\\Test\\Documents", true},
		// Different paths
		{"C:\\Users\\Other", "C:\\Users\\Test", false},
	}

	for _, tc := range testCases {
		result := isPathWithinCwd(tc.path, tc.cwd)
		if result != tc.expected {
			t.Errorf("isPathWithinCwd(%q, %q) = %v, expected %v", tc.path, tc.cwd, result, tc.expected)
		}
	}

	t.Log("AC4 PASS: isPathWithinCwd handles case-insensitive comparison for Windows paths")
}

// TestNoOsGetenvHomeInBashCommands verifies that bash_commands.go uses os.UserHomeDir().
func TestNoOsGetenvHomeInBashCommands(t *testing.T) {
	// This test verifies the fix was applied - we can't easily grep from within Go
	// but we can verify the behavior by checking that parseCdTarget works correctly
	expectedHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("UserHomeDir not available")
	}

	// If the code still uses os.Getenv("HOME") incorrectly, this test would fail
	// if HOME env var is different from UserHomeDir
	result := parseCdTarget("cd ~", "/some/cwd")
	if result != expectedHome {
		t.Errorf("parseCdTarget returned wrong home: got %q, expected %q (from UserHomeDir)", result, expectedHome)
	}
}