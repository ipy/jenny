package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtMD1RemoveMaxTokensOverride verifies that the dead setMaxTokensOverride
// code has been removed and the binary still functions correctly.
//
// Spec: debt-mD-1 — Remove dead setMaxTokensOverride code from Client and all
// four provider types.
//
// Key behavioral assertions:
// 1. Binary builds and runs without error.
// 2. Default max_tokens is 64000 for claude-sonnet-4-6 (unchanged behavior).
// 3. API requests have correct max_tokens field (proves ResolveMaxTokens still works).
// 4. No setMaxTokensOverride or maxTokensOverride strings exist in the binary.
func TestDebtMD1RemoveMaxTokensOverride(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-md1.max-tokens.default-64000",
			Category:    "debt-md1",
			Description: "default max_tokens is 64000 for claude-sonnet-4-6 after removing override code",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 64000,
					},
				},
			},
		},
		{
			ID:          "debt-md1.max-tokens.opus-default",
			Category:    "debt-md1",
			Description: "default max_tokens is 128000 for claude-opus-4-6 after removing override code",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=claude-opus-4-6"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 128000,
					},
				},
			},
		},
		{
			ID:          "debt-md1.max-tokens.haiku-default",
			Category:    "debt-md1",
			Description: "default max_tokens is 64000 for claude-haiku-4-6 after removing override code",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=claude-haiku-4-6"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 64000,
					},
				},
			},
		},
		{
			ID:          "debt-md1.exit-code-zero",
			Category:    "debt-md1",
			Description: "binary exits with code 0 for a simple prompt after removing override code",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "debt-md1.stream-json-output-valid",
			Category:    "debt-md1",
			Description: "stream-json output is valid NDJSON after removing override code",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON:   true,
					SessionIDConsistent: true,
					UUIDsUnique:         true,
					EventCount: &harness.EventCountExpectation{
						Min: 3,
					},
					HasEventTypes: []string{"system", "assistant", "result"},
				},
			},
		},
		{
			ID:          "debt-md1.api-request-has-tools",
			Category:    "debt-md1",
			Description: "API request still includes tools after removing override code",
			Tags:        []string{"debt"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							MinCount: 3,
							HasTool:  []string{"Read", "Bash"},
						},
					},
				},
			},
		},
	})
}

// TestDebtMD1BinaryNoDeadCode checks that the dead code strings are not present
// in the compiled binary.
func TestDebtMD1BinaryNoDeadCode(t *testing.T) {
	// Find the binary
	binPath := os.Getenv("JENNY_BIN")
	if binPath == "" {
		// Build it
		tmpDir := t.TempDir()
		binPath = filepath.Join(tmpDir, "jenny")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/jenny")
		cmd.Dir = findRepoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build failed: %v\n%s", err, out)
		}
	}

	// Check for dead code strings in the binary
	cmd := exec.Command("strings", binPath)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("strings command failed: %v", err)
	}

	output := string(out)

	deadStrings := []string{
		"maxTokensOverride",
		"setMaxTokensOverride",
	}

	for _, s := range deadStrings {
		if containsString(output, s) {
			t.Errorf("dead code string %q found in binary — should have been removed", s)
		}
	}
}

// findRepoRoot walks up from the test file directory to find go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// containsString checks if a substring exists in a string.
func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && searchString(haystack, needle)
}

func searchString(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
