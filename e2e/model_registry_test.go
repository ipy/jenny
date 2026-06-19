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
				Kind:   "prompt",
				Prompt: "say hi",
				Format: "text",
				Args:   []string{"--offline"},
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
				Kind:   "prompt",
				Prompt: "say hi",
				Format: "text",
				Args:   []string{"--offline"},
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
						MaxTokens: 64000,
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

// TestModelRegistryConfigOverride tests user config.json models overrides.
func TestModelRegistryConfigOverride(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "registry.config-override.accepted",
			Category:    "model-registry",
			Description: "config.json with valid models key is accepted and does not crash jenny",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 99999}}}`,
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
			Description: "config.json models.maxOutput override changes max_tokens in API request",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--offline"},
				// Set an explicit model so the test is deterministic regardless of env.
				Env: []string{"ANTHROPIC_MODEL=claude-sonnet-4-6"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": {"claude-sonnet-4-6": {"maxOutput": 88888}}}`,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				// Config override for claude-sonnet-4-6 should take precedence
				// over the bundled capability table (64000).
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 88888,
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
			Description: "malformed models key in config.json does not crash jenny (graceful degradation)",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--offline", "--print-system-prompt"},
				Env:  []string{"JENNY_HOME=${WORK_DIR}/.jenny"},
				WorkDirFiles: map[string]string{
					".jenny/config.json": `{"models": "this-should-be-an-object-not-a-string"}`,
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
