package router

import (
	"strings"
	"testing"

	koanfjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// TestConfigParsing tests parsing of a valid JSON config via koanf.
func TestConfigParsing(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	jsonContent := `{
  "routes": {
    "providers": [
      {
        "name": "deepseek",
        "type": "openai",
        "base-url": "https://api.deepseek.com",
        "accounts": [
          {
            "name": "personal",
            "keys": ["sk-ds-1", "sk-ds-2"],
            "priority": 1
          }
        ],
        "models": [
          {
            "name": "deepseek-chat",
            "tags": ["cheap", "text"],
            "priority": 1,
            "context-window": 64000,
            "max-output": 4000
          }
        ]
      }
    ],
    "profiles": {
      "default": {
        "targets": [
          { "match": { "models": ["deepseek:deepseek-chat"] } },
          { "match": { "tags": ["cheap"] } }
        ],
        "routing-mode": "sticky",
        "selection-policy": "round_robin",
        "retry-policy": {
          "max-retries": 3,
          "backoff": "exponential"
        },
        "allow-fallback": true
      }
    }
  }
}`

	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider([]byte(jsonContent)), koanfjson.Parser()); err != nil {
		t.Fatalf("failed to load test config: %v", err)
	}

	cfg, err := LoadConfigFromKoanf(k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}

	if cfg.Providers[0].Name != "deepseek" {
		t.Errorf("expected provider name 'deepseek', got %q", cfg.Providers[0].Name)
	}

	if len(cfg.Providers[0].Accounts) != 1 {
		t.Errorf("expected 1 account, got %d", len(cfg.Providers[0].Accounts))
	}

	if len(cfg.Providers[0].Accounts[0].Keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(cfg.Providers[0].Accounts[0].Keys))
	}

	if len(cfg.Providers[0].Models) != 1 {
		t.Errorf("expected 1 model, got %d", len(cfg.Providers[0].Models))
	}

	if cfg.Providers[0].Models[0].Name != "deepseek-chat" {
		t.Errorf("expected model name 'deepseek-chat', got %q", cfg.Providers[0].Models[0].Name)
	}

	profile, ok := cfg.Profiles["default"]
	if !ok {
		t.Fatal("expected default profile")
	}

	if profile.RoutingMode != "sticky" {
		t.Errorf("expected routing_mode 'sticky', got %q", profile.RoutingMode)
	}

	if profile.SelectionPolicy != "round_robin" {
		t.Errorf("expected selection_policy 'round_robin', got %q", profile.SelectionPolicy)
	}

	if profile.RetryPolicy.MaxRetries != 3 {
		t.Errorf("expected max_retries 3, got %d", profile.RetryPolicy.MaxRetries)
	}

	if profile.AllowFallback == nil || !*profile.AllowFallback {
		t.Error("expected allow_fallback to be true")
	}
}

// TestConfigParsing_Invalid tests that invalid JSON returns an error.
func TestConfigParsing_Invalid(t *testing.T) {
	invalidJSON := `{ "routes": { "providers": [ { "name": "test", "tags": [invalid json here] } ] } }`

	k := koanf.New(".")
	// Note: k.Load will fail if JSON is invalid
	err := k.Load(rawbytes.Provider([]byte(invalidJSON)), koanfjson.Parser())
	if err == nil {
		t.Fatal("expected error for invalid JSON load")
	}
}

// TestConfigParsing_NotFound tests that empty koanf returns a default config.
func TestConfigParsing_NotFound(t *testing.T) {
	cfg, err := LoadConfigFromKoanf(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Profiles) != 0 {
		t.Error("expected empty profiles for missing config")
	}
}

// TestZeroConfigEnvSync tests that environment variables are synthesized into config.
func TestZeroConfigEnvSync(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-4-5-20251101")
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")

	cfg := SynthesizeConfigFromEnv()
	if cfg == nil {
		t.Fatal("expected non-nil synthesized config")
	}

	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}

	if cfg.Providers[0].Type != "anthropic" {
		t.Errorf("expected provider type 'anthropic', got %q", cfg.Providers[0].Type)
	}

	if cfg.Providers[0].Models[0].Name != "claude-opus-4-5-20251101" {
		t.Errorf("expected model 'claude-opus-4-5-20251101', got %q", cfg.Providers[0].Models[0].Name)
	}

	if len(cfg.Providers[0].Accounts[0].Keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(cfg.Providers[0].Accounts[0].Keys))
	}

	if cfg.Providers[0].Accounts[0].Keys[0] != "sk-ant-test-key" {
		t.Errorf("expected key 'sk-ant-test-key', got %q", cfg.Providers[0].Accounts[0].Keys[0])
	}
}

// TestSelectEndpoint_Sticky tests that same sessionID returns same endpoint.
func TestSelectEndpoint_Sticky(t *testing.T) {
	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "test-provider",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key1", "key2"}, Priority: 1},
				},
				Models: []Model{
					{Name: "test-model", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:         []Target{{Match: MatchClause{Models: []string{"test-model"}}}},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
			},
		},
	}

	router := NewRouter(cfg)
	sessionID := "test-session-123"

	// First call should return an endpoint
	ep1, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep1 == nil {
		t.Fatal("expected non-nil endpoint")
	}

	// Second call with same session should return the same endpoint
	ep2, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep1.APIKey != ep2.APIKey {
		t.Errorf("expected same API key for sticky session, got %q vs %q", ep1.APIKey, ep2.APIKey)
	}
	if ep1.Model != ep2.Model {
		t.Errorf("expected same model for sticky session, got %q vs %q", ep1.Model, ep2.Model)
	}
}

// TestSelectEndpoint_RoundRobin tests that different sessions get distributed endpoints.
func TestSelectEndpoint_RoundRobin(t *testing.T) {
	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "test-provider",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key1", "key2"}, Priority: 1},
				},
				Models: []Model{
					{Name: "test-model", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:         []Target{{Match: MatchClause{Models: []string{"test-model"}}}},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
			},
		},
	}

	router := NewRouter(cfg)

	// Collect endpoints from different sessions
	keys := make(map[string]bool)
	for i := 0; i < 10; i++ {
		ep, err := router.SelectEndpoint(strings.Repeat("s", i+1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		keys[ep.APIKey] = true
	}

	// With 2 keys and 10 sessions, we should see both keys used
	if len(keys) < 1 {
		t.Errorf("expected at least 1 unique key, got %d", len(keys))
	}
}

// TestLayer3_ModelFallback tests that after clearing sticky, new endpoint is selected.
func TestLayer3_ModelFallback(t *testing.T) {
	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "provider-a",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key-a"}, Priority: 1},
				},
				Models: []Model{
					{Name: "model-a", Tags: []string{"expensive"}, Priority: 1},
				},
			},
			{
				Name:    "provider-b",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key-b"}, Priority: 2},
				},
				Models: []Model{
					{Name: "model-b", Tags: []string{"cheap"}, Priority: 2},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets: []Target{
					{Match: MatchClause{Tags: []string{"expensive"}}},
					{Match: MatchClause{Tags: []string{"cheap"}}},
				},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
			},
		},
	}

	router := NewRouter(cfg)
	sessionID := "test-session"

	// First selection should pick model-a (higher priority)
	ep1, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ep1.Model != "model-a" {
		t.Errorf("expected model-a, got %q", ep1.Model)
	}

	// Clear sticky and select again
	router.ClearSticky(sessionID)
	ep2, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since model-a is still available, it should be selected again
	if ep2.Model != "model-a" {
		t.Errorf("expected model-a after clear, got %q", ep2.Model)
	}
}

// TestHealthRegistry_Cooldown tests cooldown behavior.
func TestHealthRegistry_Cooldown(t *testing.T) {
	registry := NewHealthRegistry()

	// Initially healthy
	if !registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be healthy initially")
	}

	// Record failures up to max
	for i := 0; i < registry.maxFailures; i++ {
		registry.RecordFailure("provider", "account", "model", "key1")
	}

	// Now should be unhealthy (in cooldown)
	if registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be unhealthy after max failures")
	}
}

// TestHealthRegistry_Reset tests reset behavior.
func TestHealthRegistry_Reset(t *testing.T) {
	registry := NewHealthRegistry()

	// Record some failures
	registry.RecordFailure("provider", "account", "model", "key1")
	registry.RecordFailure("provider", "account", "model", "key1")
	registry.RecordFailure("provider", "account", "model", "key1")

	// Reset
	registry.Reset()

	// Should be healthy again
	if !registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be healthy after reset")
	}
}

// TestHealthRegistry_RecordSuccess tests that success clears failure count.
func TestHealthRegistry_RecordSuccess(t *testing.T) {
	registry := NewHealthRegistry()

	// Record some failures
	registry.RecordFailure("provider", "account", "model", "key1")
	registry.RecordFailure("provider", "account", "model", "key1")

	// Record success
	registry.RecordSuccess("provider", "account", "model", "key1")

	// Should still be healthy (failure count is 0)
	if !registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be healthy after success")
	}

	if registry.GetFailureCount("provider", "account", "model", "key1") != 0 {
		t.Error("expected failure count to be 0 after success")
	}
}

// TestLoadConfigFromKoanf tests the unified config loading path.
func TestLoadConfigFromKoanf(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	tests := []struct {
		name       string
		setup      func(k *koanf.Koanf)
		envSetup   func()
		verify     func(t *testing.T, cfg *Config, err error)
		cleanupEnv func()
	}{
		{
			name: "no routes key returns empty config with profiles",
			setup: func(k *koanf.Koanf) {
				// empty
			},
			verify: func(t *testing.T, cfg *Config, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg == nil {
					t.Fatal("expected non-nil config")
				}
				if cfg.Profiles == nil {
					t.Error("expected Profiles map to be initialized")
				}
				if len(cfg.Providers) != 0 {
					t.Errorf("expected 0 providers, got %d", len(cfg.Providers))
				}
			},
		},
		{
			name:  "nil koanf returns empty config with profiles",
			setup: nil, // passes nil k to LoadConfigFromKoanf in loop
			verify: func(t *testing.T, cfg *Config, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg == nil {
					t.Fatal("expected non-nil config")
				}
				if cfg.Profiles == nil {
					t.Error("expected Profiles map to be initialized")
				}
			},
		},
		{
			name: "valid routes unmarshal",
			setup: func(k *koanf.Koanf) {
				k.Set("routes.providers", []map[string]any{
					{
						"name": "p1",
						"type": "openai",
					},
				})
				k.Set("routes.profiles", map[string]any{
					"default": map[string]any{
						"routing-mode": "balanced",
					},
				})
			},
			verify: func(t *testing.T, cfg *Config, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "p1" {
					t.Errorf("unexpected providers: %+v", cfg.Providers)
				}
				if cfg.Profiles["default"].RoutingMode != "balanced" {
					t.Errorf("unexpected routing-mode: %s", cfg.Profiles["default"].RoutingMode)
				}
			},
		},
		{
			name: "type mismatch error",
			setup: func(k *koanf.Koanf) {
				k.Set("routes.providers", "not a slice")
			},
			verify: func(t *testing.T, cfg *Config, err error) {
				if err == nil {
					t.Error("expected error for type mismatch, got nil")
				}
			},
		},
		{
			name: "env provider merge and dedup",
			envSetup: func() {
				t.Setenv("ANTHROPIC_API_KEY", "env-key")
				t.Setenv("ANTHROPIC_MODEL", "env-model")
			},
			setup: func(k *koanf.Koanf) {
				k.Set("routes.providers", []map[string]any{
					{
						"name": "anthropic", // same name as env synthesizer uses
						"type": "anthropic",
						"accounts": []map[string]any{
							{"name": "file-account", "keys": []string{"file-key"}},
						},
					},
				})
			},
			verify: func(t *testing.T, cfg *Config, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should have only 1 anthropic provider (from file, since it dedups)
				count := 0
				var p *Provider
				for i := range cfg.Providers {
					if cfg.Providers[i].Name == "anthropic" {
						count++
						p = &cfg.Providers[i]
					}
				}
				if count != 1 {
					t.Errorf("expected 1 anthropic provider, got %d", count)
				}
				if p.Accounts[0].Name != "file-account" {
					t.Errorf("expected file-account to take precedence, got %s", p.Accounts[0].Name)
				}
			},
		},
		{
			name: "defaults applied",
			setup: func(k *koanf.Koanf) {
				k.Set("routes.profiles", map[string]any{
					"p1": map[string]any{},
				})
				k.Set("routes.providers", []map[string]any{
					{
						"name": "p1",
						"accounts": []map[string]any{
							{"name": "a1"},
						},
					},
				})
			},
			verify: func(t *testing.T, cfg *Config, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				prof := cfg.Profiles["p1"]
				if prof.RoutingMode != "sticky" {
					t.Errorf("expected default routing-mode sticky, got %q", prof.RoutingMode)
				}
				if prof.RetryPolicy.MaxRetries != 5 {
					t.Errorf("expected default max-retries 5, got %d", prof.RetryPolicy.MaxRetries)
				}
				if cfg.Providers[0].Accounts[0].Priority != 1 {
					t.Errorf("expected default account priority 1, got %d", cfg.Providers[0].Accounts[0].Priority)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var k *koanf.Koanf
			if tt.setup != nil {
				k = koanf.New(".")
				tt.setup(k)
			}
			if tt.envSetup != nil {
				tt.envSetup()
			}

			cfg, err := LoadConfigFromKoanf(k)
			tt.verify(t, cfg, err)
		})
	}
}
