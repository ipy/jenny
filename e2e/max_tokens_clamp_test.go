package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestMaxTokensClamp verifies the resolveMaxTokens behavior through e2e tests.
// It checks that max_tokens in outbound API requests matches the model's
// capability table and that unknown models get the conservative default.
func TestMaxTokensClamp(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "max-tokens-clamp.default-sonnet-capability",
			Category:    "max-tokens-clamp",
			Description: "default max_tokens for claude-sonnet-4-6 is 64000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "claude-sonnet-4-6"},
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
			ID:          "max-tokens-clamp.opus-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for claude-opus-4-* is 128000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "claude-opus-4-5-20251101"},
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
			ID:          "max-tokens-clamp.haiku-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for claude-haiku-4-* is 64000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "claude-haiku-4-5"},
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
			ID:          "max-tokens-clamp.unknown-model-conservative-default",
			Category:    "max-tokens-clamp",
			Description: "unknown model gets conservative default of 16384 with warning",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "totally-unknown-model-v1"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 16384,
					},
				},
				Stderr: &harness.StderrExpectation{
					Contains: []string{"unknown_model_capability_default"},
				},
			},
		},
		{
			ID:          "max-tokens-clamp.gpt-5-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for gpt-5* is 128000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "gpt-5"},
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
			ID:          "max-tokens-clamp.gpt-4o-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for gpt-4o* is 16384 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "gpt-4o"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 16384,
					},
				},
			},
		},
		{
			ID:          "max-tokens-clamp.deepseek-v4-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for deepseek-v4-* is 384000 (regression guard against stale 8192)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "deepseek-v4-1"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 384000,
					},
				},
			},
		},
		{
			ID:          "max-tokens-clamp.gemini-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for gemini-2.5-* is 65536 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "gemini-2.5-pro"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 65536,
					},
				},
			},
		},
		{
			ID:          "max-tokens-clamp.fable-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for claude-fable-5* is 128000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "claude-fable-5-1"},
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
			ID:          "max-tokens-clamp.o3-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for o3* is 100000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "o3"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 100000,
					},
				},
			},
		},
		{
			ID:          "max-tokens-clamp.o4-mini-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for o4-mini* is 100000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "o4-mini"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 100000,
					},
				},
			},
		},
		{
			ID:          "max-tokens-clamp.gpt-4.1-capability",
			Category:    "max-tokens-clamp",
			Description: "max_tokens for gpt-4.1* is 33000 (model capability)",
			Tags:        []string{"max-tokens-clamp"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--model", "gpt-4.1"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 33000,
					},
				},
			},
		},
	})
}
