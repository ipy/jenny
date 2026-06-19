package tool

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/v2"
)

// WebSearchProviderStrategy selects the search path.
type WebSearchProviderStrategy string

const (
	StrategyNative   WebSearchProviderStrategy = "native"
	StrategyClient   WebSearchProviderStrategy = "client"
	StrategyDisabled WebSearchProviderStrategy = "disabled"
)

// WebSearchConfig holds the resolved web search configuration.
type WebSearchConfig struct {
	Strategy     WebSearchProviderStrategy `koanf:"provider"`
	ClientConfig ClientConfig              `koanf:"client"`
}

// ClientConfig holds configuration for a client-side search provider.
type ClientConfig struct {
	Provider string `koanf:"provider"` // "tavily" or "custom"
	APIKey   string `koanf:"api-key"`
	BaseURL  string `koanf:"base-url"` // custom provider endpoint
}

// DefaultWebSearchConfig returns the default configuration.
func DefaultWebSearchConfig() WebSearchConfig {
	return WebSearchConfig{
		Strategy: StrategyNative,
	}
}

// ResolveWebSearchConfig reads web search configuration from koanf using nested keys
// matching the spec's JSON structure:
//
//	{
//	  "web-search": {
//	    "provider": "native|client|disabled",
//	    "client": {
//	      "provider": "tavily|custom",
//	      "api-key": "env:NAME|literal:VALUE|plain",
//	      "base-url": "https://..."
//	    }
//	  }
//	}
//
// Precedence: env vars > JSON config > defaults.
// Env vars are auto-mapped by koanf: JENNY_WEB_SEARCH_PROVIDER -> web-search-provider, etc.
// Both nested (web-search.provider) and flat (web-search-provider) keys are supported.
func ResolveWebSearchConfig(k *koanf.Koanf) WebSearchConfig {
	cfg := DefaultWebSearchConfig()

	// Use Cut to access the nested "web-search" subtree (JSON config).
	ws := k.Cut("web-search")

	// Read flat keys from root first (for env var precedence: JENNY_WEB_SEARCH_PROVIDER -> web-search-provider),
	// then fall back to nested subtree (JSON config: "web-search": {"provider": ...}).
	// This ensures env vars (loaded after JSON) take precedence.
	if v := k.String("web-search-provider"); v != "" {
		cfg.Strategy = WebSearchProviderStrategy(v)
	} else if v := ws.String("provider"); v != "" {
		cfg.Strategy = WebSearchProviderStrategy(v)
	}

	if v := k.String("web-search-client-provider"); v != "" {
		cfg.ClientConfig.Provider = v
	} else if v := ws.String("client.provider"); v != "" {
		cfg.ClientConfig.Provider = v
	}

	if v := ResolveClientAPIKey(k, ws); v != "" {
		cfg.ClientConfig.APIKey = v
	}

	if v := k.String("web-search-client-base-url"); v != "" {
		cfg.ClientConfig.BaseURL = v
	} else if v := ws.String("client.base-url"); v != "" {
		cfg.ClientConfig.BaseURL = v
	}

	return cfg
}

// ResolveClientAPIKey resolves a client API key, checking flat root keys first
// (for env var precedence) then nested subtree (for JSON config).
// Supports three forms:
//   - env:NAME — read from environment variable NAME
//   - literal:VALUE — literal value
//   - plain literal (any other string)
func ResolveClientAPIKey(k *koanf.Koanf, ws *koanf.Koanf) string {
	raw := k.String("web-search-client-api-key")
	if raw == "" {
		raw = ws.String("client.api-key")
	}
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "env:") {
		return os.Getenv(strings.TrimPrefix(raw, "env:"))
	}
	if strings.HasPrefix(raw, "literal:") {
		return strings.TrimPrefix(raw, "literal:")
	}
	return raw
}

// ValidateWebSearchConfig checks that the configuration is valid.
func ValidateWebSearchConfig(cfg WebSearchConfig) error {
	switch cfg.Strategy {
	case StrategyNative, StrategyClient, StrategyDisabled:
		// valid
	default:
		return fmt.Errorf("invalid web search strategy %q: must be native, client, or disabled", cfg.Strategy)
	}

	if cfg.ClientConfig.Provider != "" {
		if cfg.Strategy != StrategyClient {
			return fmt.Errorf("client provider is configured but strategy is not 'client'")
		}
		if cfg.ClientConfig.Provider != "tavily" && cfg.ClientConfig.Provider != "custom" {
			return fmt.Errorf("invalid client provider %q: must be tavily or custom", cfg.ClientConfig.Provider)
		}
		if cfg.ClientConfig.APIKey == "" {
			return fmt.Errorf("client API key is required when strategy is client")
		}
	}

	return nil
}

// NewSearchClientProvider creates a SearchClientProvider from configuration.
func NewSearchClientProvider(cfg ClientConfig) (SearchClientProvider, error) {
	switch cfg.Provider {
	case "tavily":
		return NewTavilyProvider(cfg.APIKey), nil
	case "custom":
		return NewCustomProvider(cfg.BaseURL, cfg.APIKey), nil
	default:
		return nil, fmt.Errorf("unknown client provider: %s", cfg.Provider)
	}
}
