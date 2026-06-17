// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config represents the top-level routing configuration.
type Config struct {
	Providers []Provider         `koanf:"providers"`
	Profiles  map[string]Profile `koanf:"profiles"`
}

// LoadConfig loads the router configuration from a YAML file.
// If path is empty, it defaults to ~/.jenny/routes.yaml.
// If an explicit path is provided and it does not exist, returns (nil, nil).
// Environment-synthesized providers are merged into the config, and defaults are applied.
func LoadConfig(path string) (*Config, error) {
	var cfg *Config
	isDefaultPath := false

	if path == "" {
		isDefaultPath = true
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, ".jenny", "routes.yaml")
	}

	// Check if file exists
	if _, err := os.Stat(path); err == nil {
		k := koanf.New(".")
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("failed to load router config from %q: %w", path, err)
		}

		cfg = &Config{}
		if err := k.Unmarshal("", cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal router config: %w", err)
		}
	} else if os.IsNotExist(err) {
		if !isDefaultPath {
			return nil, nil // Test expects nil for explicit missing path
		}
	} else {
		return nil, fmt.Errorf("failed to stat router config: %w", err)
	}

	// Fall back to empty config if file doesn't exist (so we can merge from env)
	if cfg == nil {
		cfg = &Config{Profiles: make(map[string]Profile)}
	}

	// Merge environment-synthesized providers.
	envProviders := SynthesizeConfigFromEnv()
	if envProviders != nil {
		mergeEnvProviders(cfg, envProviders.Providers)
	}

	// Apply defaults.
	applyDefaults(cfg)

	return cfg, nil
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
