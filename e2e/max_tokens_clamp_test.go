package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestMaxTokensClamp verifies max_tokens in outbound API requests.
// With dynamic max_tokens, turn 1 uses the earlyTurnCap which varies by model:
//   - Models with maxOut >= 100K (thinking models): 16384
//   - Smaller models: min(4096, modelMax)
func TestMaxTokensClamp(t *testing.T) {
	models := []struct {
		id       string
		name     string
		expected int
	}{
		// Large models (maxOut >= 100K) → earlyTurnCap = 16384
		{"claude-sonnet-4-6", "default-sonnet-capability", 16384},
		{"claude-opus-4-5-20251101", "opus-capability", 16384},
		{"claude-haiku-4-5", "haiku-capability", 16384},
		{"gpt-5", "gpt-5-capability", 16384},
		{"deepseek-v4-1", "deepseek-v4-capability", 16384},
		{"claude-fable-5-1", "fable-capability", 16384},
		{"o3", "o3-capability", 16384},
		{"o4-mini", "o4-mini-capability", 16384},

		// Medium models → earlyTurnCap = 4096
		{"gpt-4o", "gpt-4o-capability", 4096},
		{"gemini-2.5-pro", "gemini-capability", 4096},
		{"gpt-4.1", "gpt-4.1-capability", 4096},

		// Unknown model (fallback maxOut=16384 < 100K) → 4096
		{"totally-unknown-model-v1", "unknown-model-conservative-default", 4096},
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
