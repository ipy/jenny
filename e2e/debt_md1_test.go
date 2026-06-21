package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

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

// TestDebtMD1RemoveMaxTokensOverride verifies that max_tokens is correctly
// applied in API requests. With dynamic max_tokens, turn 1 uses the discovery
// budget (2000) regardless of model capability.
func TestDebtMD1RemoveMaxTokensOverride(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-md1.max-tokens.default-64000",
			Category:    "debt-md1",
			Description: "turn 1 max_tokens is 2000 (discovery budget) for claude-sonnet-4-6",
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
						MaxTokens: 2000,
					},
				},
			},
		},
		{
			ID:          "debt-md1.max-tokens.opus-default",
			Category:    "debt-md1",
			Description: "turn 1 max_tokens is 2000 (discovery budget) for claude-opus-4-6",
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
						MaxTokens: 2000,
					},
				},
			},
		},
		{
			ID:          "debt-md1.max-tokens.haiku-default",
			Category:    "debt-md1",
			Description: "turn 1 max_tokens is 2000 (discovery budget) for claude-haiku-4-6",
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
						MaxTokens: 2000,
					},
				},
			},
		},
		{
			ID:          "debt-md1.exit-code-zero",
			Category:    "debt-md1",
			Description: "binary exits with code 0 for a simple prompt",
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
			Description: "stream-json output is valid NDJSON",
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
			Description: "API request still includes tools",
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
