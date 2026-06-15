package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// makeTwoKeyProvider returns a config with a single provider/account containing
// two healthy keys. Used to verify true round-robin distribution.
func makeTwoKeyProvider() *Config {
	return &Config{
		Providers: []Provider{
			{
				Name:    "test-provider",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key-a", "key-b"}, Priority: 1},
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
}

// TestRoundRobin_DistributesAcrossSessions asserts that with N sessions and M
// keys, at least min(N, M) unique keys are selected (true cross-session
// distribution, not the old hash-on-session-id behavior).
func TestRoundRobin_DistributesAcrossSessions(t *testing.T) {
	r := NewRouter(makeTwoKeyProvider())

	seen := make(map[string]int)
	for i := range 20 {
		ep, err := r.SelectEndpoint("session-" + itoa(i))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[ep.APIKey]++
	}
	if len(seen) < 2 {
		t.Errorf("expected round-robin to use both keys across 20 sessions, got %d unique", len(seen))
	}
}

// TestBalanced_ReevaluatesPerTurn asserts that with routing_mode=balanced,
// every call to SelectEndpoint re-evaluates (does not stick to the first hit).
func TestBalanced_ReevaluatesPerTurn(t *testing.T) {
	cfg := makeTwoKeyProvider()
	cfg.Profiles["default"] = Profile{
		Targets:         []Target{{Match: MatchClause{Models: []string{"test-model"}}}},
		RoutingMode:     "balanced",
		SelectionPolicy: "round_robin",
	}
	r := NewRouter(cfg)

	// balanced should NOT return the cached sticky endpoint on a second call
	// from the same session — it should re-evaluate via the candidate pool.
	seen := make(map[string]bool)
	for range 10 {
		ep, err := r.SelectEndpoint("same-session")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[ep.APIKey] = true
	}
	if len(seen) < 2 {
		t.Errorf("balanced mode should rotate keys across calls, got %d unique", len(seen))
	}
}

// TestLoadConfig_EnvMergeWithYAML asserts that when a YAML config exists, env
// variables do NOT silently replace it. Instead, providers synthesized from env
// are appended to the YAML's providers list (per spec: "Router may still allow
// merging environment-based keys into a temporary legacy provider for debugging").
func TestLoadConfig_EnvMergeWithYAML(t *testing.T) {
	yamlContent := `
providers:
  - name: "yaml-provider"
    type: "openai"
    base_url: "https://api.example.com"
    accounts:
      - name: "default"
        keys: ["yaml-key"]
    models:
      - name: "yaml-model"
profiles:
  default:
    targets:
      - match: { models: ["yaml-model"] }
`
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-merge")
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-4-5-20251101")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	hasYAML := false
	hasLegacy := false
	for _, p := range cfg.Providers {
		if p.Name == "yaml-provider" {
			hasYAML = true
		}
		if p.Name == "legacy-anthropic" {
			hasLegacy = true
		}
	}
	if !hasYAML {
		t.Error("yaml-provider missing after merge")
	}
	if !hasLegacy {
		t.Error("legacy-anthropic provider not merged from env")
	}
}

// TestSticky_401DoesNotRetry asserts that a 401 (Invalid Key) is treated as
// permanent and not retried on the same key — the spec's Layer 1 says retries
// are for 429/5xx only. 401s should escalate immediately to Layer 2.
func TestSticky_401DoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	// Point the OpenAI provider at the mock server so ensureClient()
	// doesn't connect to a real API that may hang.
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	t.Setenv("OPENAI_API_KEY", "sk-test-key")

	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "test",
				Type:    "openai",
				BaseURL: srv.URL,
				Accounts: []Account{
					{Name: "default", Keys: []string{"k1", "k2"}, Priority: 1},
				},
				Models: []Model{
					{Name: "m", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:       []Target{{Match: MatchClause{Models: []string{"m"}}}},
				RoutingMode:   "sticky",
				RetryPolicy:   RetryPolicy{MaxRetries: 3, Backoff: "exponential"},
				AllowFallback: new(true),
			},
		},
	}
	r := NewRouter(cfg)
	sc := NewStickyClient("s1", r)
	// SendMessage should NOT loop 3 times on the same key for 401.
	_, _ = sc.SendMessage(t.Context(), nil, nil, nil, nil, "")

	// We expect exactly 1 call to the first key, then the layer-2 failover
	// to k2, then failure. Total <= 2.
	if got := calls.Load(); got > 2 {
		t.Errorf("expected at most 2 HTTP calls on 401 (no L1 retry), got %d", got)
	}
}

// itoa is a tiny helper to avoid pulling in strconv at the test top.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// guard against strings import drift in IDEs
var _ = strings.HasPrefix
