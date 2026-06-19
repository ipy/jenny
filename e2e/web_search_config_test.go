package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestWebSearchConfigValidation verifies that ValidateWebSearchConfig
// produces validation errors when ClientConfig.Provider is set but the
// strategy is not "client". This tests the fix for debt-mD-2.
func TestWebSearchConfigValidation(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "websearch.config.native-with-client-provider",
			Category:    "websearch-config",
			Description: "StrategyNative + ClientConfig.Provider set produces validation warning",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_WEB_SEARCH_PROVIDER=native",
					"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
					"JENNY_WEB_SEARCH_CLIENT_API_KEY=test-key",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"client provider is configured but strategy is not 'client'"},
				},
			},
		},
		{
			ID:          "websearch.config.disabled-with-client-provider",
			Category:    "websearch-config",
			Description: "StrategyDisabled + ClientConfig.Provider set produces validation warning",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_WEB_SEARCH_PROVIDER=disabled",
					"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
					"JENNY_WEB_SEARCH_CLIENT_API_KEY=test-key",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"client provider is configured but strategy is not 'client'"},
				},
			},
		},
		{
			ID:          "websearch.config.client-with-provider-no-warning",
			Category:    "websearch-config",
			Description: "StrategyClient + ClientConfig.Provider set does NOT produce validation warning",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_WEB_SEARCH_PROVIDER=client",
					"JENNY_WEB_SEARCH_CLIENT_PROVIDER=tavily",
					"JENNY_WEB_SEARCH_CLIENT_API_KEY=test-key",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					NotContains: []string{"client provider is configured but strategy is not 'client'"},
				},
			},
		},
		{
			ID:          "websearch.config.native-without-client-provider",
			Category:    "websearch-config",
			Description: "StrategyNative without client provider does NOT produce validation warning",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_WEB_SEARCH_PROVIDER=native",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stderr: &harness.StderrExpectation{
					NotContains: []string{"client provider is configured but strategy is not 'client'"},
				},
			},
		},
	})
}
