// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"os"
	"sort"

	"github.com/ipy/jenny/internal/api"
)

// SynthesizeConfigFromEnv creates a Config from standard environment variables,
// sorted by priority (Anthropic > OpenAI > GenAI). This is the Zero-Config path
// and supplements any file-based providers in the config.
func SynthesizeConfigFromEnv() *Config {
	cfg := &Config{
		Providers: []Provider{},
		Profiles:  make(map[string]Profile),
	}

	// Check OpenAI
	if openAIKey := os.Getenv("OPENAI_API_KEY"); openAIKey != "" {
		model := os.Getenv("OPENAI_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		cfg.Providers = append(cfg.Providers, Provider{
			Name:    "openai",
			Type:    "openai",
			BaseURL: baseURL,
			Accounts: []Account{{
				Name:     "default",
				Keys:     []string{openAIKey},
				Priority: 2,
			}},
			Models: []Model{{
				Name:          model,
				Tags:          []string{},
				Priority:      2,
				ContextWindow: 128000,
				MaxOutput:     16384,
			}},
		})
	}

	// Check GenAI (Gemini/Vertex AI)
	if api.IsGenAIEnvSet() {
		model := os.Getenv("GENAI_MODEL")
		if model == "" {
			model = os.Getenv("GEMINI_MODEL")
		}
		if model == "" {
			model = "gemini-2.0-flash"
		}
		baseURL := os.Getenv("GENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}

		var apiKeys []string
		if key := os.Getenv("GENAI_API_KEY"); key != "" {
			apiKeys = append(apiKeys, key)
		}
		if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKeys = append(apiKeys, key)
		}
		if key := os.Getenv("GEMINI_API_KEY"); key != "" {
			apiKeys = append(apiKeys, key)
		}

		if len(apiKeys) > 0 {
			cfg.Providers = append(cfg.Providers, Provider{
				Name:    "genai",
				Type:    "gemini",
				BaseURL: baseURL,
				Accounts: []Account{{
					Name:     "default",
					Keys:     apiKeys,
					Priority: 3,
				}},
				Models: []Model{{
					Name:          model,
					Tags:          []string{},
					Priority:      3,
					ContextWindow: 1000000,
					MaxOutput:     8192,
				}},
			})
		}
	}

	// Check Anthropic
	// Use consolidated detection (same as client.NewClientWithModel) to avoid drift.
	if api.IsAnthropicEnvSet() {
		model := os.Getenv("ANTHROPIC_MODEL")
		if model == "" {
			model = "claude-opus-4-5-20251101"
		}
		baseURL := os.Getenv("ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}

		// Prefer API key for the account, fall back to auth token.
		var keys []string
		if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
			keys = append(keys, apiKey)
		}
		if authToken := os.Getenv("ANTHROPIC_AUTH_TOKEN"); authToken != "" {
			keys = append(keys, authToken)
		}
		// At least one of API_KEY or AUTH_TOKEN is guaranteed to exist here,
		// because IsAnthropicEnvSet checks for them; when only BASE_URL is
		// present we still create the provider so it can be used with no-key
		// setups (e.g. local proxies that inject the key).

		cfg.Providers = append(cfg.Providers, Provider{
			Name:    "anthropic",
			Type:    "anthropic",
			BaseURL: baseURL,
			Accounts: []Account{{
				Name:     "default",
				Keys:     keys,
				Priority: 1,
			}},
			Models: []Model{{
				Name:          model,
				Tags:          []string{},
				Priority:      1,
				ContextWindow: 200000,
				MaxOutput:     20000,
			}},
		})
	}

	// Sort providers by account priority
	sort.Slice(cfg.Providers, func(i, j int) bool {
		return cfg.Providers[i].Accounts[0].Priority < cfg.Providers[j].Accounts[0].Priority
	})

	// Create default profile
	defaultAllow := true
	cfg.Profiles["default"] = Profile{
		RoutingMode:     "sticky",
		SelectionPolicy: "round_robin",
		RetryPolicy: RetryPolicy{
			MaxRetries: 3,
			Backoff:    "exponential",
		},
		AllowFallback: &defaultAllow,
	}

	return cfg
}
