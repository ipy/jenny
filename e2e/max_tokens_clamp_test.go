package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestMaxTokensClamp verifies max_tokens in outbound API requests.
// With fixed thinking budget, turn 1 total = fixedThinking(modelMax*60/100)
// + earlyCompletion(>=100K ? 4096 : 2048).
func TestMaxTokensClamp(t *testing.T) {
	models := []struct {
		id       string
		name     string
		expected int
	}{
		// Large models (maxOut >= 100K): fixedThinking + 4096
		{"claude-opus-4-5-20251101", "opus-capability", 80896}, // 128000*60/100 + 4096
		{"gpt-5", "gpt-5-capability", 80896},                   // 128000*60/100 + 4096
		{"deepseek-v4-1", "deepseek-v4-capability", 234496},    // 384000*60/100 + 4096 (note: deepseek-v4-1 resolves to 384000)
		{"claude-fable-5-1", "fable-capability", 80896},        // 128000*60/100 + 4096
		{"o3", "o3-capability", 64096},                         // 100000*60/100 + 4096
		{"o4-mini", "o4-mini-capability", 64096},               // 100000*60/100 + 4096

		// Medium/small models (maxOut < 100K): fixedThinking + 2048
		{"claude-sonnet-4-6", "default-sonnet-capability", 40448}, // 64000*60/100 + 2048
		{"claude-haiku-4-5", "haiku-capability", 40448},           // 64000*60/100 + 2048
		{"gpt-4o", "gpt-4o-capability", 11878},                    // 16384*60/100 + 2048
		{"gemini-2.5-pro", "gemini-capability", 41369},            // 65536*60/100 + 2048
		{"gpt-4.1", "gpt-4.1-capability", 21848},                  // 33000*60/100 + 2048

		// Unknown model (fallback maxOut=16384 < 100K)
		{"totally-unknown-model-v1", "unknown-model-conservative-default", 11878}, // 16384*60/100 + 2048
	}

	var cases []*harness.TestCase
	for _, m := range models {
		cases = append(cases, &harness.TestCase{
			ID:          "max-tokens-clamp." + m.name,
			Category:    "max-tokens-clamp",
			Description: "turn 1 max_tokens matches earlyTurnCap for " + m.id,
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", m.id},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: m.expected,
					},
				},
			},
		})
	}
	runE2ESuite(t, cases)
}

// TestMaxTokensClampTools verifies that API requests include tool definitions.
func TestMaxTokensClampTools(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{{
		ID:          "max-tokens-clamp.api-request-has-tools",
		Category:    "max-tokens-clamp",
		Description: "API request includes tools with expected types",
		Tags:        []string{"max-tokens-clamp"},
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
	}})
}

// TestMaxTokensClampDefaultModel verifies that the default model (no env override)
// produces a valid non-zero max_tokens in the API request.
func TestMaxTokensClampDefaultModel(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{{
		ID:          "max-tokens-clamp.default-model-no-env",
		Category:    "max-tokens-clamp",
		Description: "default model (no ANTHROPIC_MODEL set) produces valid max_tokens",
		Tags:        []string{"max-tokens-clamp"},
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
					Index:    0,
					HasField: []string{"max_tokens"},
				},
			},
		},
	}})
}
