package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestMaxTokensClamp verifies max_tokens in outbound API requests.
// With dynamic max_tokens, turn 1 always uses the discovery budget (2000)
// regardless of model capability. The model capability feeds into
// MaxTokensController.modelMax for later turns.
func TestMaxTokensClamp(t *testing.T) {
	models := []struct {
		id   string
		name string
	}{
		{"claude-sonnet-4-6", "default-sonnet-capability"},
		{"claude-opus-4-5-20251101", "opus-capability"},
		{"claude-haiku-4-5", "haiku-capability"},
		{"totally-unknown-model-v1", "unknown-model-conservative-default"},
		{"gpt-5", "gpt-5-capability"},
		{"gpt-4o", "gpt-4o-capability"},
		{"deepseek-v4-1", "deepseek-v4-capability"},
		{"gemini-2.5-pro", "gemini-capability"},
		{"claude-fable-5-1", "fable-capability"},
		{"o3", "o3-capability"},
		{"o4-mini", "o4-mini-capability"},
		{"gpt-4.1", "gpt-4.1-capability"},
	}

	var cases []*harness.TestCase
	for _, m := range models {
		cases = append(cases, &harness.TestCase{
			ID:          "max-tokens-clamp." + m.name,
			Category:    "max-tokens-clamp",
			Description: "turn 1 max_tokens is 2000 (discovery budget) for " + m.id,
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
						MaxTokens: 2000,
					},
				},
			},
		})
	}
	runE2ESuite(t, cases)
}
