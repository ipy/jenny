package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNormalize_NoProviderNameStringsInProduction verifies that provider name strings
// (minimax, deepseek) do not appear in production code paths as conditionals.
// Legitimate uses (capability tables, model name strings, comments) are allowed.
func TestNormalize_NoProviderNameStringsInProduction(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..")
	cmd := exec.Command("grep", "-rin", "minimax\\|deepseek",
		filepath.Join(root, "internal", "api"),
		filepath.Join(root, "internal", "agent"),
		"--include=*.go")
	output, _ := cmd.CombinedOutput()

	var violations []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if line == "" {
			continue
		}
		// Skip test files
		if strings.Contains(line, "_test.go") {
			continue
		}
		// Skip transformer.go — it is the designated file for backend-specific transforms
		if strings.Contains(line, "transformer.go") {
			continue
		}
		// Skip comments
		if strings.Contains(line, "//") {
			continue
		}
		// Skip model name strings in data/config (pricing tables, capability maps)
		if strings.Contains(line, `"deepseek`) || strings.Contains(line, `"minimax`) ||
			strings.Contains(line, `"MiniMax`) || strings.Contains(line, `"DeepSeek`) {
			continue
		}
		violations = append(violations, line)
	}

	if len(violations) > 0 {
		t.Errorf("provider name conditionals found in production code:\n%s", strings.Join(violations, "\n"))
	}
}

// TestNormalize_UniversalArchitectureCrossLinked locks in the docs cross-link to
// universal-normalization-architecture.md. The spec requires README.md, when
// present, to reference the architecture doc. This test fails if the link is removed
// from docs/README.md.
func TestNormalize_UniversalArchitectureCrossLinked(t *testing.T) {
	// Find project root: the directory containing go.mod, two levels up from internal/api.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..")
	readme := filepath.Join(root, "docs", "README.md")

	contents, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("read %s: %v", readme, err)
	}

	if !strings.Contains(string(contents), "universal-normalization-architecture.md") {
		t.Errorf("docs/README.md must cross-link to universal-normalization-architecture.md")
	}
}
