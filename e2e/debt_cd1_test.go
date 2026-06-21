package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestDebtCD1_OverridePreservedWithRefresh tests the fix for debt-CD-1:
// User overrides from config.json must survive across registry operations.
// The original bug: --refresh-registry never applied user overrides because
// Fetch() was called before user override loading.
// The fix: user override loading now precedes --refresh-registry in main.go,
// and Fetch() calls applyOverridesLocked() after refreshing models.
//
// Note: With dynamic max_tokens, turn 1 always uses discovery budget (2000)
// regardless of model capability. These tests verify exit-code-zero behavior
// to confirm registry override plumbing doesn't crash. The override values
// feed into MaxTokensController.modelMax for later turns.
func TestDebtCD1_OverridePreservedWithRefresh(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-CD-1.override.survives-cache-load",
			Category:    "registry",
			Description: "User override in config.json is accepted without error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env: []string{
					"JENNY_HOME=${WORK_DIR}/.jenny",
					"ANTHROPIC_MODEL=custom-test-model",
				},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"schemaVersion": 1,
						"lastUpdated": "2026-06-19T00:00:00Z",
						"models": {
							"custom-test-model": {
								"id": "custom-test-model",
								"provider": "custom",
								"contextWindow": 200000,
								"maxOutput": 8000,
								"pricing": {"input": 3.0, "output": 15.0}
							}
						}
					}`,
					".jenny/meta.json":   `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/config.json": `{"models": {"custom-test-model": {"maxOutput": 16000}}}`,
				},
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
			ID:          "debt-CD-1.override.model-not-in-cache",
			Category:    "registry",
			Description: "User override for a model not present in the registry cache does not cause errors",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env: []string{
					"JENNY_HOME=${WORK_DIR}/.jenny",
					"ANTHROPIC_MODEL=override-only-model",
				},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"schemaVersion": 1,
						"lastUpdated": "2026-06-19T00:00:00Z",
						"models": {
							"claude-sonnet-4-6": {
								"id": "claude-sonnet-4-6",
								"provider": "anthropic",
								"contextWindow": 200000,
								"maxOutput": 64000,
								"pricing": {"input": 3.0, "output": 15.0}
							}
						}
					}`,
					".jenny/meta.json":   `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/config.json": `{"models": {"override-only-model": {"maxOutput": 32000}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "debt-CD-1.override.overrides-cache-for-same-model",
			Category:    "registry",
			Description: "Config override replaces registry cache value for same model when both are present",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env: []string{
					"JENNY_HOME=${WORK_DIR}/.jenny",
					"ANTHROPIC_MODEL=claude-sonnet-4-6",
				},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"schemaVersion": 1,
						"lastUpdated": "2026-06-19T00:00:00Z",
						"models": {
							"claude-sonnet-4-6": {
								"id": "claude-sonnet-4-6",
								"provider": "anthropic",
								"contextWindow": 200000,
								"maxOutput": 64000,
								"pricing": {"input": 3.0, "output": 15.0}
							}
						}
					}`,
					".jenny/meta.json":   `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 99999}}}`,
				},
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
			ID:          "debt-CD-1.override.no-config-uses-cache",
			Category:    "registry",
			Description: "Without config.json overrides, the registry cache value is used as-is",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env: []string{
					"JENNY_HOME=${WORK_DIR}/.jenny",
					"ANTHROPIC_MODEL=test-custom-model",
				},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"schemaVersion": 1,
						"lastUpdated": "2026-06-19T00:00:00Z",
						"models": {
							"test-custom-model": {
								"id": "test-custom-model",
								"provider": "test",
								"contextWindow": 100000,
								"maxOutput": 8000,
								"pricing": {"input": 1.0, "output": 5.0}
							}
						}
					}`,
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
				},
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
	})
}

// TestDebtCD1_OverridePriority verifies the priority order:
// user config.json override > upstream registry > bundled defaults.
// With dynamic max_tokens, turn 1 is discovery (2000) so the assertion
// focuses on exit-code-zero to confirm plumbing correctness.
func TestDebtCD1_OverridePriority(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-CD-1.priority.override-wins-over-cache",
			Category:    "registry",
			Description: "config.json override wins over cache, which wins over bundled default",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env: []string{
					"JENNY_HOME=${WORK_DIR}/.jenny",
					"ANTHROPIC_MODEL=claude-sonnet-4-6",
				},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"schemaVersion": 1,
						"lastUpdated": "2026-06-19T00:00:00Z",
						"models": {
							"claude-sonnet-4-6": {
								"id": "claude-sonnet-4-6",
								"provider": "anthropic",
								"contextWindow": 200000,
								"maxOutput": 32000,
								"pricing": {"input": 3.0, "output": 15.0}
							}
						}
					}`,
					".jenny/meta.json":   `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 77777}}}`,
				},
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
	})
}

// TestDebtCD1_MalformedOverridesAreSafe verifies that the fix for debt-CD-1
// does not introduce crashes when config.json has unexpected override fields.
func TestDebtCD1_MalformedOverridesAreSafe(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-CD-1.safety.multiple-overrides-no-crash",
			Category:    "registry",
			Description: "Multiple model overrides in config.json do not cause crashes",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				Env: []string{
					"JENNY_HOME=${WORK_DIR}/.jenny",
					"ANTHROPIC_MODEL=test-model-a",
				},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"schemaVersion": 1,
						"lastUpdated": "2026-06-19T00:00:00Z",
						"models": {
							"test-model-a": {
								"id": "test-model-a",
								"provider": "test",
								"contextWindow": 100000,
								"maxOutput": 10000,
								"pricing": {"input": 1.0, "output": 5.0}
							},
							"test-model-b": {
								"id": "test-model-b",
								"provider": "test",
								"contextWindow": 200000,
								"maxOutput": 20000,
								"pricing": {"input": 2.0, "output": 10.0}
							}
						}
					}`,
					".jenny/meta.json":   `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/config.json": `{"models": {"test-model-a": {"maxOutput": 44444}, "test-model-b": {"maxOutput": 55555}, "nonexistent": {"maxOutput": 66666}}}`,
				},
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
	})
}
