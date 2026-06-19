package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtMD8ModelCapConsolidation verifies that model capability lookup works
// correctly after consolidating modelMaxOutputCap to delegate to lookupModelCap.
// The refactoring should not change any observable behavior — max_tokens in API
// requests must remain correct for known models, and unknown models must fall
// back to the default.
func TestDebtMD8ModelCapConsolidation(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-MD-8.build.succeeds",
			Category:    "debt-MD-8",
			Description: "binary builds and runs with no errors after consolidation",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--version"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "debt-MD-8.max-tokens.claude-sonnet-4-6",
			Category:    "debt-MD-8",
			Description: "max_tokens is 64000 for claude-sonnet-4-6 (bundled table lookup)",
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
			ID:          "debt-MD-8.max-tokens.claude-opus-4-5",
			Category:    "debt-MD-8",
			Description: "max_tokens is 128000 for claude-opus-4-5-20251101 (bundled table lookup)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=claude-opus-4-5-20251101"},
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
			ID:          "debt-MD-8.max-tokens.claude-haiku-4-5",
			Category:    "debt-MD-8",
			Description: "max_tokens is 64000 for claude-haiku-4-5-20251001 (bundled table lookup)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=claude-haiku-4-5-20251001"},
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
			ID:          "debt-MD-8.max-tokens.default-model",
			Category:    "debt-MD-8",
			Description: "default model (no ANTHROPIC_MODEL set) produces a valid max_tokens in API request",
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
						// The default model should get a valid max_tokens (non-zero)
						HasField: []string{"max_tokens"},
					},
				},
			},
		},
		{
			ID:          "debt-MD-8.max-tokens.unknown-model-fallback",
			Category:    "debt-MD-8",
			Description: "unknown model falls back to unknownModelMaxTokens (16384) in API request",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=completely-unknown-model-xyz"},
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
			ID:          "debt-MD-8.stream-json.has-result-event",
			Category:    "debt-MD-8",
			Description: "stream-json output is well-formed after consolidation",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env:      []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"system", "assistant", "result"},
				},
			},
		},
		{
			ID:          "debt-MD-8.cli.print-system-prompt",
			Category:    "debt-MD-8",
			Description: "--print-system-prompt works correctly after consolidation",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
				Env:  []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "debt-MD-8.cli.help-flag",
			Category:    "debt-MD-8",
			Description: "--help flag writes usage information to stderr after consolidation",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--help"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"Usage"},
				},
			},
		},
	})
}
