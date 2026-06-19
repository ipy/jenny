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
// Because --refresh-registry fetches from an external URL (no env var to
// override), we verify the override semantics indirectly:
//   1. User override takes effect even when registry cache is present
//   2. Overrides for models not in the cache don't cause errors
//   3. config.json with valid overrides produces the expected max_tokens
func TestDebtCD1_OverridePreservedWithRefresh(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "debt-CD-1.override.survives-cache-load",
			Category:    "registry",
			Description: "User override in config.json takes precedence over cached registry values",
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
					// Cache uses flat map format: {"<model-id>": {...}}
					// with maxOutput: 8000 for custom-test-model
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
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					// config.json overrides maxOutput to 16000
					".jenny/config.json": `{"models": {"custom-test-model": {"maxOutput": 16000}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				// The override value (16000) should be used, not the cache value (8000).
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 16000,
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
					// Cache does NOT have override-only-model
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
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					// config.json defines override-only-model even though it's not in the cache
					".jenny/config.json": `{"models": {"override-only-model": {"maxOutput": 32000}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 32000,
					},
				},
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
					// Cache has maxOutput: 64000 for claude-sonnet-4-6
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
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					// config.json overrides maxOutput to 99999
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 99999}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 99999,
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
					// Cache has maxOutput: 8000 for test-custom-model (not in bundled defaults)
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
					// No config.json — should use cache value directly
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 8000,
					},
				},
			},
		},
	})
}

// TestDebtCD1_OverridePriority verifies the priority order:
// user config.json override > upstream registry > bundled defaults
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
					// Cache has maxOutput: 32000 (different from bundled default 64000)
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
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					// Override maxOutput to 77777
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 77777}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				// Override value 77777 should win over cache value 32000 (which would
				// itself win over bundled default 64000).
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 77777,
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
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					// Multiple overrides, only one affects the current model
					".jenny/config.json": `{"models": {"test-model-a": {"maxOutput": 44444}, "test-model-b": {"maxOutput": 55555}, "nonexistent": {"maxOutput": 66666}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 44444,
					},
				},
			},
		},
	})
}
