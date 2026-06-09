package api

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNormalize_NoProviderNameStringsInProduction verifies that provider name strings
// (minimax, deepseek) do not appear in production code paths.
func TestNormalize_NoProviderNameStringsInProduction(t *testing.T) {
	// Run grep to find minimax or deepseek in production Go files
	cmd := exec.Command("grep", "-rin", "minimax\\|deepseek",
		"internal/api/",
		"internal/agent/",
		"--include=*.go")
	output, _ := cmd.CombinedOutput()

	// Filter out test files and comments
	var violations []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if line == "" {
			continue
		}
		// Skip test files
		if strings.Contains(line, "_test.go") {
			continue
		}
		// Skip comments-only lines (lines starting with //)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		violations = append(violations, line)
	}

	if len(violations) > 0 {
		t.Errorf("provider name strings found in production code:\n%s", strings.Join(violations, "\n"))
	}
}
