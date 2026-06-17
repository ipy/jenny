// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Config represents the top-level routing configuration.
type Config struct {
	Providers []Provider         `koanf:"providers"`
	Profiles  map[string]Profile `koanf:"profiles"`
}

// Provider defines a backend provider with credentials and models.
type Provider struct {
	Name     string    `koanf:"name"`
	Type     string    `koanf:"type"` // openai, anthropic, gemini
	BaseURL  string    `koanf:"base-url"`
	Accounts []Account `koanf:"accounts"`
	Models   []Model   `koanf:"models"`
}

// Account represents a set of API keys for a provider.
type Account struct {
	Name     string   `koanf:"name"`
	Keys     []string `koanf:"keys"`
	Priority int      `koanf:"priority"`
}

// Model represents a model configuration within a provider.
type Model struct {
	Name          string   `koanf:"name"`
	Tags          []string `koanf:"tags"`
	Priority      int      `koanf:"priority"`
	ContextWindow int      `koanf:"context-window"`
	MaxOutput     int      `koanf:"max-output"`
}

// Profile defines an execution policy with target chains and routing behavior.
type Profile struct {
	Targets         []Target    `koanf:"targets"`
	RoutingMode     string      `koanf:"routing-mode"`     // sticky, balanced
	SelectionPolicy string      `koanf:"selection-policy"` // round_robin, random
	RetryPolicy     RetryPolicy `koanf:"retry-policy"`
	AllowFallback   *bool       `koanf:"allow-fallback"`
}

// Target represents a matching rule within a profile's target chain.
type Target struct {
	Match MatchClause `koanf:"match"`
}

// MatchClause defines what models or tags a target matches.
type MatchClause struct {
	Models []string `koanf:"models"`
	Tags   []string `koanf:"tags"`
}

// RetryPolicy defines retry behavior for a profile.
type RetryPolicy struct {
	MaxRetries int    `koanf:"max-retries"`
	Backoff    string `koanf:"backoff"` // exponential, linear
}

// ResolveKeys resolves secret references in all accounts.
// Key interpretation rules:
//   - "env:NAME" — load via os.Getenv("NAME"). Empty result → error.
//   - "literal:VALUE" — use VALUE as the literal API key.
//   - "VALUE" (unprefixed) — use VALUE as the literal API key.
func (cfg *Config) ResolveKeys() error {
	for i := range cfg.Providers {
		for j := range cfg.Providers[i].Accounts {
			resolved := make([]string, 0, len(cfg.Providers[i].Accounts[j].Keys))
			for _, key := range cfg.Providers[i].Accounts[j].Keys {
				if strings.HasPrefix(key, "env:") {
					val := os.Getenv(key[4:])
					if val == "" {
						return fmt.Errorf("environment variable %q not set (required by provider %q account %q)",
							key[4:], cfg.Providers[i].Name, cfg.Providers[i].Accounts[j].Name)
					}
					resolved = append(resolved, val)
				} else if strings.HasPrefix(key, "literal:") {
					resolved = append(resolved, key[8:])
				} else {
					resolved = append(resolved, key)
				}
			}
			cfg.Providers[i].Accounts[j].Keys = resolved
		}
	}
	return nil
}

// LoadConfigFromKoanf unmarshals the routes configuration from a koanf instance.
// If no "routes" key is present, returns an empty config with an empty Profiles map.
// After unmarshalling, merges env-synthesized providers and applies defaults before returning.
func LoadConfigFromKoanf(k *koanf.Koanf) (*Config, error) {
	cfg := &Config{Profiles: make(map[string]Profile)}

	// Try to unmarshal the routes key; if it doesn't exist, proceed with empty config
	if k != nil && k.Exists("routes") {
		if err := k.Unmarshal("routes", cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal router config: %w", err)
		}
	}

	// Merge environment-synthesized providers (file-based providers take precedence).
	envProviders := SynthesizeConfigFromEnv()
	if envProviders != nil {
		mergeEnvProviders(cfg, envProviders.Providers)
	}

	// Apply defaults.
	applyDefaults(cfg)

	return cfg, nil
}

// mergeEnvProviders appends env-derived providers to the config unless a
// provider with the same name already exists.
func mergeEnvProviders(cfg *Config, envProviders []Provider) {
	for _, ep := range envProviders {
		exists := false
		for _, p := range cfg.Providers {
			if p.Name == ep.Name {
				exists = true
				break
			}
		}
		if !exists {
			cfg.Providers = append(cfg.Providers, ep)
		}
	}
}

// applyDefaults sets default values for omitted configuration fields.
func applyDefaults(cfg *Config) {
	for i := range cfg.Providers {
		for j := range cfg.Providers[i].Accounts {
			if cfg.Providers[i].Accounts[j].Priority == 0 {
				cfg.Providers[i].Accounts[j].Priority = 1
			}
		}
		for j := range cfg.Providers[i].Models {
			if cfg.Providers[i].Models[j].Priority == 0 {
				cfg.Providers[i].Models[j].Priority = 1
			}
		}
	}

	for name, profile := range cfg.Profiles {
		if profile.RoutingMode == "" {
			profile.RoutingMode = "sticky"
		}
		if profile.SelectionPolicy == "" {
			profile.SelectionPolicy = "round_robin"
		}
		if profile.RetryPolicy.MaxRetries == 0 {
			profile.RetryPolicy.MaxRetries = 5
		}
		if profile.RetryPolicy.Backoff == "" {
			profile.RetryPolicy.Backoff = "exponential"
		}
		if profile.AllowFallback == nil {
			defaultAllow := true
			profile.AllowFallback = &defaultAllow
		}
		cfg.Profiles[name] = profile
	}
}
