package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestModelRegistryRefreshFlag verifies the --refresh-registry flag behavior.
func TestModelRegistryRefreshFlag(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.refresh-flag.recognized-and-exits-nonzero",
			Category:    "model-registry",
			Description: "--refresh-registry flag is recognized, attempts fetch, exits non-zero on network failure",
			Target: harness.TargetInvocation{
				Kind:      "cli",
				Args:      []string{"--refresh-registry", "-p", "hello"},
				TimeoutMs: 120000,
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"registry", "refresh", "fetch", "network", "error", "failed"},
				},
			},
		},
	})
}

// TestModelRegistryOfflineFlag verifies the --offline flag behavior.
func TestModelRegistryOfflineFlag(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.offline-flag.recognized",
			Category:    "model-registry",
			Description: "--offline flag is recognized and accepted",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "text",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "registry.offline-flag.accepted-without-network",
			Category:    "model-registry",
			Description: "--offline exits 0 even without network",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "text",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

// TestModelRegistryCacheBehavior tests cache file behavior with pre-provisioned fixtures.
func TestModelRegistryCacheBehavior(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.cache.fresh-cache-no-fetch",
			Category:    "model-registry",
			Description: "with fresh cache and --offline, jenny uses cache without error",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `{
						"_meta": {},
						"providers": {},
						"models": {
							"anthropic": [{
								"id": "claude-sonnet-4-6",
								"provider": "anthropic",
								"contextWindow": 200000,
								"maxOutput": 64000,
								"pricing": {"input": 3.0, "output": 15.0},
								"modalities": {"input": ["text"], "output": ["text"]},
								"abilities": {"reasoning": true}
							}]
						}
					}`,
					".jenny/meta.json": `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 1000},
				},
			},
		},
		{
			ID:          "registry.bundled-default.provides-64000",
			Category:    "model-registry",
			Description: "bundled capability table provides max_tokens for the configured model (spec SC10/SC11)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				// Set an explicit model so the test is deterministic regardless of env.
				Env: []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 4096,
					},
				},
			},
		},
		{
			ID:          "registry.cache.missing-cache-offline-works",
			Category:    "model-registry",
			Description: "with no cache and --offline, jenny still starts (falls back to bundled defaults)",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config": "", // ensure .jenny dir exists but no models.json
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
	})
}

// TestModelRegistryConfigOverride tests user settings.json models overrides.
func TestModelRegistryConfigOverride(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.config-override.accepted",
			Category:    "model-registry",
			Description: "settings.json with valid models key is accepted and does not crash jenny",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 99999}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.config-override.affects-max-tokens",
			Category:    "model-registry",
			Description: "settings.json models.maxOutput override changes max_tokens in API request",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				// Set an explicit model so the test is deterministic regardless of env.
				Env: []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 88888}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				// The config override sets modelMax=88888, which lowers the
				// auto-compact threshold enough that the full system prompt
				// triggers compact before the first turn. Request[0] is the
				// compact summary (SUMMARY_MAX_TOKENS=20000). Request[1] is
				// the normal agent turn (earlyTurnCap=min(4096,88888)=4096).
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 20000,
					},
					{
						Index:     1,
						MaxTokens: 4096,
					},
				},
			},
		},
	})
}

// TestModelRegistryMalformedConfig tests malformed config handling.
func TestModelRegistryMalformedConfig(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.malformed-config.no-crash",
			Category:    "model-registry",
			Description: "malformed models key in settings.json does not crash jenny (graceful degradation)",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": "this-should-be-an-object-not-a-string"}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
	})
}

// TestModelRegistryCorruptedCache tests corrupted cache file handling.
func TestModelRegistryCorruptedCache(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.corrupted-cache.survives",
			Category:    "model-registry",
			Description: "corrupted models.json does not crash jenny; it falls back to defaults",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/models.json": `this is not valid json at all {{{`,
					".jenny/meta.json":   `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
	})
}

// TestModelRegistryOfflineNoNetwork tests that --offline prevents all network access attempts.
func TestModelRegistryOfflineNoNetwork(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.offline.no-fetch-warning",
			Category:    "model-registry",
			Description: "--offline skips fetch without network error messages",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config": "",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					NotContains: []string{"TLS handshake", "connection refused", "no such host", "network error"},
				},
			},
		},
	})
}

// TestModelRegistryConfigWarnings verifies that malformed models in settings.json
// produce warnings on stderr when --verbose is used.
func TestModelRegistryConfigWarnings(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.malformed-models-warns-on-stderr",
			Category:    "model-registry",
			Description: "malformed models key in settings.json produces a warning on stderr with --verbose",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt", "--verbose"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": "not-an-object"}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"model", "config", "parse", "warn", "error", "invalid", "malformed", "models"},
				},
			},
		},
	})
}

// TestModelRegistryConfigEdgeCases tests settings.json edge cases that don't crash.
func TestModelRegistryConfigEdgeCases(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.absent-models-key-works",
			Category:    "model-registry",
			Description: "settings.json without models key works correctly",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"output-format": "text"}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.empty-config-works",
			Category:    "model-registry",
			Description: "empty settings.json works correctly",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.empty-models-key-works",
			Category:    "model-registry",
			Description: "settings.json with empty models object does not crash",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": {}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.no-config-file-works",
			Category:    "model-registry",
			Description: "no settings.json file at all does not crash",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config": "",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
	})
}

// TestModelRegistryMalformedModelTypes verifies that various non-object types
// for the models key in settings.json do not crash jenny.
func TestModelRegistryMalformedModelTypes(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.models-as-array-no-crash",
			Category:    "model-registry",
			Description: "models key as array does not crash jenny",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": ["this", "is", "an", "array"]}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.models-as-number-no-crash",
			Category:    "model-registry",
			Description: "models key as number does not crash jenny",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": 42}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.models-as-boolean-no-crash",
			Category:    "model-registry",
			Description: "models key as boolean does not crash jenny",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": true}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
		{
			ID:          "registry.models-as-null-no-crash",
			Category:    "model-registry",
			Description: "models key as null does not crash jenny",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/settings.json": `{"models": null}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 500},
				},
			},
		},
	})
}

// TestModelRegistryOverridePriority verifies that user settings.json overrides
// survive across registry operations and take priority over cache values.
func TestModelRegistryOverridePriority(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.override.survives-cache-load",
			Category:    "model-registry",
			Description: "user override in settings.json is accepted when registry cache exists",
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
					".jenny/meta.json":     `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/settings.json": `{"models": {"custom-test-model": {"maxOutput": 16000}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{Index: 0},
				},
			},
		},
		{
			ID:          "registry.override.model-not-in-cache",
			Category:    "model-registry",
			Description: "user override for a model not present in the registry cache does not cause errors",
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
					".jenny/meta.json":     `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/settings.json": `{"models": {"override-only-model": {"maxOutput": 32000}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
		{
			ID:          "registry.override.overrides-cache-for-same-model",
			Category:    "model-registry",
			Description: "config override replaces registry cache value for same model when both are present",
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
					".jenny/meta.json":     `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/settings.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 99999}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{Index: 0},
				},
			},
		},
		{
			ID:          "registry.override.no-config-uses-cache",
			Category:    "model-registry",
			Description: "without settings.json overrides, the registry cache value is used as-is",
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
					{Index: 0},
				},
			},
		},
		{
			ID:          "registry.override.priority-override-wins-over-cache",
			Category:    "model-registry",
			Description: "settings.json override wins over cache, which wins over bundled default",
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
					".jenny/meta.json":     `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/settings.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 77777}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{Index: 0},
				},
			},
		},
		{
			ID:          "registry.override.multiple-overrides-no-crash",
			Category:    "model-registry",
			Description: "multiple model overrides in settings.json do not cause crashes",
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
					".jenny/meta.json":     `{"fetchedAt": "2026-06-19T00:00:00Z", "etag": "test-etag", "schemaVersion": 1}`,
					".jenny/settings.json": `{"models": {"test-model-a": {"maxOutput": 44444}, "test-model-b": {"maxOutput": 55555}, "nonexistent": {"maxOutput": 66666}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{Index: 0},
				},
			},
		},
	})
}
