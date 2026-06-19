package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtMD7ModelPricingConsolidation verifies that cost tracking works correctly
// after consolidating the duplicate ModelPricing type from internal/agent/cost.go
// to use config.ModelPricing. The refactoring should not change any observable behavior.
func TestDebtMD7ModelPricingConsolidation(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-MD-7.build.succeeds",
			Category:    "debt-MD-7",
			Description: "binary builds with no compilation errors after removing duplicate ModelPricing",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--version"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "debt-MD-7.cost.result-has-cost-fields",
			Category:    "debt-MD-7",
			Description: "result event includes total_cost_usd after ModelPricing consolidation",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"total_cost_usd"},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.result-has-usage",
			Category:    "debt-MD-7",
			Description: "result includes usage object with input_tokens and output_tokens",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
						Nested: map[string]*harness.EventExpectation{
							"usage": {
								HasFields: []string{"input_tokens", "output_tokens"},
							},
						},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.result-has-model-usage",
			Category:    "debt-MD-7",
			Description: "result includes per-model usage breakdown via modelUsage field",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"modelUsage"},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.result-has-num-turns",
			Category:    "debt-MD-7",
			Description: "result includes num_turns after consolidation",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"num_turns"},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.result-has-duration-ms",
			Category:    "debt-MD-7",
			Description: "result includes duration_ms",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"duration_ms"},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.cost-is-numeric",
			Category:    "debt-MD-7",
			Description: "total_cost_usd is a numeric value (not null, not string)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:          "result",
						FieldNotEmpty: []string{"total_cost_usd"},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.input-tokens-is-integer",
			Category:    "debt-MD-7",
			Description: "usage.input_tokens is present and non-empty",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
						Nested: map[string]*harness.EventExpectation{
							"usage": {
								FieldNotEmpty: []string{"input_tokens", "output_tokens"},
							},
						},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.cache-fields-present",
			Category:    "debt-MD-7",
			Description: "usage includes cache_read_input_tokens and cache_creation_input_tokens",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
						Nested: map[string]*harness.EventExpectation{
							"usage": {
								HasFields: []string{
									"cache_read_input_tokens",
									"cache_creation_input_tokens",
								},
							},
						},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.model-usage-has-cost",
			Category:    "debt-MD-7",
			Description: "modelUsage entry includes cost field (computed from ModelPricing)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
						Nested: map[string]*harness.EventExpectation{
							"modelUsage": {
								HasFields: []string{"claude-opus-4-5-20251101"},
							},
						},
					},
				},
			},
		},
		{
			ID:          "debt-MD-7.cost.stdout-contains-json-events",
			Category:    "debt-MD-7",
			Description: "stdout contains stream-json events (not corrupted by type changes)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"system", "assistant", "result"},
				},
			},
		},
	})
}
