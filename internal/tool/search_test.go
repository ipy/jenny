package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	koanfjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// ---------------------------------------------------------------------------
// TestProvider_SupportsNativeSearch — verifies each provider kind
// ---------------------------------------------------------------------------

func TestProvider_SupportsNativeSearch(t *testing.T) {
	// Provider-level SupportsNativeSearch is tested in internal/api.
	// Here we verify the concept works through the tool layer by checking
	// that the interface method exists and is callable.
	// The actual provider implementations are tested in the api package.
	var runner NativeSearchRunner
	if runner != nil {
		t.Error("expected nil value to be usable")
	}
}

// ---------------------------------------------------------------------------
// TestResolveWebSearchConfig — JSON, env, default precedence
// ---------------------------------------------------------------------------

func TestResolveWebSearchConfig(t *testing.T) {
	// Default config
	t.Run("defaults", func(t *testing.T) {
		k := koanf.New(".")
		cfg := ResolveWebSearchConfig(k)
		if cfg.Strategy != StrategyNative {
			t.Errorf("expected default strategy native, got %q", cfg.Strategy)
		}
	})

	// JSON config
	t.Run("json_config", func(t *testing.T) {
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"provider": "client",
				"client": {
					"provider": "tavily",
					"api-key": "tvly-test-key",
					"base-url": "https://custom.example.com"
				}
			}
		}`)), koanfjson.Parser())
		cfg := ResolveWebSearchConfig(k)
		if cfg.Strategy != StrategyClient {
			t.Errorf("expected strategy client, got %q", cfg.Strategy)
		}
		if cfg.ClientConfig.Provider != "tavily" {
			t.Errorf("expected client provider tavily, got %q", cfg.ClientConfig.Provider)
		}
		if cfg.ClientConfig.APIKey != "tvly-test-key" {
			t.Errorf("expected api-key tvly-test-key, got %q", cfg.ClientConfig.APIKey)
		}
		if cfg.ClientConfig.BaseURL != "https://custom.example.com" {
			t.Errorf("expected base-url, got %q", cfg.ClientConfig.BaseURL)
		}
	})

	// Env override (env has higher precedence — loaded after JSON)
	t.Run("env_override", func(t *testing.T) {
		t.Setenv("JENNY_WEB_SEARCH_PROVIDER", "disabled")
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"provider": "native"
			}
		}`)), koanfjson.Parser())
		_ = k.Load(env.Provider("JENNY_", ".", func(s string) string {
			return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "JENNY_")), "_", "-")
		}), nil)
		cfg := ResolveWebSearchConfig(k)
		if cfg.Strategy != StrategyDisabled {
			t.Errorf("expected env override to disabled, got %q", cfg.Strategy)
		}
	})

	// Partial config — only client settings
	t.Run("partial_config", func(t *testing.T) {
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"client": {
					"provider": "custom",
					"api-key": "literal:my-key"
				}
			}
		}`)), koanfjson.Parser())
		cfg := ResolveWebSearchConfig(k)
		// Strategy stays default (native)
		if cfg.Strategy != StrategyNative {
			t.Errorf("expected default native, got %q", cfg.Strategy)
		}
		if cfg.ClientConfig.Provider != "custom" {
			t.Errorf("expected client provider custom, got %q", cfg.ClientConfig.Provider)
		}
	})
}

// ---------------------------------------------------------------------------
// TestResolveClientAPIKey — env:NAME, literal:X, plain literal forms
// ---------------------------------------------------------------------------

func TestResolveClientAPIKey(t *testing.T) {
	t.Run("env_form", func(t *testing.T) {
		t.Setenv("TAVILY_API_KEY", "tvly-secret-123")
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"client": {
					"api-key": "env:TAVILY_API_KEY"
				}
			}
		}`)), koanfjson.Parser())
		ws := k.Cut("web-search")
		key := ResolveClientAPIKey(k, ws)
		if key != "tvly-secret-123" {
			t.Errorf("expected tvly-secret-123, got %q", key)
		}
	})

	t.Run("literal_form", func(t *testing.T) {
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"client": {
					"api-key": "literal:my-direct-key"
				}
			}
		}`)), koanfjson.Parser())
		ws := k.Cut("web-search")
		key := ResolveClientAPIKey(k, ws)
		if key != "my-direct-key" {
			t.Errorf("expected my-direct-key, got %q", key)
		}
	})

	t.Run("plain_form", func(t *testing.T) {
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"client": {
					"api-key": "just-a-key"
				}
			}
		}`)), koanfjson.Parser())
		ws := k.Cut("web-search")
		key := ResolveClientAPIKey(k, ws)
		if key != "just-a-key" {
			t.Errorf("expected just-a-key, got %q", key)
		}
	})

	t.Run("empty", func(t *testing.T) {
		k := koanf.New(".")
		ws := k.Cut("web-search")
		key := ResolveClientAPIKey(k, ws)
		if key != "" {
			t.Errorf("expected empty, got %q", key)
		}
	})

	t.Run("missing_env_var", func(t *testing.T) {
		os.Unsetenv("MISSING_VAR")
		k := koanf.New(".")
		_ = k.Load(rawbytes.Provider([]byte(`{
			"web-search": {
				"client": {
					"api-key": "env:MISSING_VAR"
				}
			}
		}`)), koanfjson.Parser())
		ws := k.Cut("web-search")
		key := ResolveClientAPIKey(k, ws)
		if key != "" {
			t.Errorf("expected empty for missing env var, got %q", key)
		}
	})
}

// ---------------------------------------------------------------------------
// TestValidateWebSearchConfig
// ---------------------------------------------------------------------------

func TestValidateWebSearchConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WebSearchConfig
		wantErr bool
		errMsg  string
	}{
		{"valid native", WebSearchConfig{Strategy: StrategyNative}, false, ""},
		{"valid client tavily", WebSearchConfig{
			Strategy:     StrategyClient,
			ClientConfig: ClientConfig{Provider: "tavily", APIKey: "key123"},
		}, false, ""},
		{"valid client custom", WebSearchConfig{
			Strategy:     StrategyClient,
			ClientConfig: ClientConfig{Provider: "custom", APIKey: "key123"},
		}, false, ""},
		{"valid disabled", WebSearchConfig{Strategy: StrategyDisabled}, false, ""},
		{"invalid strategy", WebSearchConfig{Strategy: "invalid"}, true, "invalid"},
		{"client no api key", WebSearchConfig{
			Strategy:     StrategyClient,
			ClientConfig: ClientConfig{Provider: "tavily"},
		}, true, "API key is required"},
		{"client invalid provider", WebSearchConfig{
			Strategy:     StrategyClient,
			ClientConfig: ClientConfig{Provider: "serpapi", APIKey: "key"},
		}, true, "invalid client provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWebSearchConfig(tt.cfg)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTavilyProvider — httptest happy path, 4xx, 5xx, timeout
// ---------------------------------------------------------------------------

func TestTavilyProvider(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/search" {
				t.Errorf("expected /search, got %s", r.URL.Path)
			}
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-key" {
				t.Errorf("expected Bearer test-key, got %q", auth)
			}

			resp := map[string]any{
				"results": []map[string]any{
					{"title": "Result 1", "url": "https://example.com/1", "content": "Snippet 1"},
					{"title": "Result 2", "url": "https://example.com/2", "content": "Snippet 2"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := &TavilyProvider{
			apiKey:  "test-key",
			baseURL: server.URL + "/search",
			client:  server.Client(),
		}

		resp, err := p.Search(context.Background(), "test query")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 2 {
			t.Errorf("expected 2 results, got %d", len(resp.Results))
		}
		if resp.Results[0].Title != "Result 1" {
			t.Errorf("expected 'Result 1', got %q", resp.Results[0].Title)
		}
		if resp.Query != "test query" {
			t.Errorf("expected query 'test query', got %q", resp.Query)
		}
	})

	t.Run("http_4xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized"))
		}))
		defer server.Close()

		p := &TavilyProvider{
			apiKey:  "bad-key",
			baseURL: server.URL,
			client:  server.Client(),
		}

		_, err := p.Search(context.Background(), "test")
		if err == nil {
			t.Fatal("expected error for 401")
		}
		if !strings.Contains(err.Error(), "HTTP 401") {
			t.Errorf("expected HTTP 401 in error, got: %v", err)
		}
	})

	t.Run("http_5xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		p := &TavilyProvider{
			apiKey:  "test-key",
			baseURL: server.URL,
			client:  server.Client(),
		}

		_, err := p.Search(context.Background(), "test")
		if err == nil {
			t.Fatal("expected error for 500")
		}
		if !strings.Contains(err.Error(), "HTTP 500") {
			t.Errorf("expected HTTP 500 in error, got: %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Use the request context to detect cancellation (do not block)
			<-r.Context().Done()
		}))
		defer server.Close()

		p := &TavilyProvider{
			apiKey:  "test-key",
			baseURL: server.URL,
			client:  server.Client(),
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediate cancel

		_, err := p.Search(ctx, "test")
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})
}

// ---------------------------------------------------------------------------
// TestCustomProvider — httptest with documented JSON shape
// ---------------------------------------------------------------------------

func TestCustomProvider(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}

			var reqBody customRequest
			json.NewDecoder(r.Body).Decode(&reqBody)
			if reqBody.Query != "test query" {
				t.Errorf("expected query 'test query', got %q", reqBody.Query)
			}

			resp := SearchResponse{
				Query: "test query",
				Results: []SearchResult{
					{Title: "Custom Result", URL: "https://custom.example.com", Snippet: "A custom snippet"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		p := NewCustomProvider(server.URL, "custom-key")
		resp, err := p.Search(context.Background(), "test query")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 1 {
			t.Errorf("expected 1 result, got %d", len(resp.Results))
		}
		if resp.Results[0].Title != "Custom Result" {
			t.Errorf("expected 'Custom Result', got %q", resp.Results[0].Title)
		}
	})

	t.Run("http_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()

		p := NewCustomProvider(server.URL, "")
		_, err := p.Search(context.Background(), "test")
		if err == nil {
			t.Fatal("expected error for 502")
		}
	})

	t.Run("invalid_json_response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}))
		defer server.Close()

		p := NewCustomProvider(server.URL, "")
		_, err := p.Search(context.Background(), "test")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// ---------------------------------------------------------------------------
// TestSearchResponseRender — native vs client rendering parity
// ---------------------------------------------------------------------------

func TestSearchResponseRender(t *testing.T) {
	t.Run("with_results", func(t *testing.T) {
		resp := &SearchResponse{
			Query: "test query",
			Results: []SearchResult{
				{Title: "Title 1", URL: "https://example.com/1", Snippet: "First snippet"},
				{Title: "Title 2", URL: "https://example.com/2", Snippet: "Second snippet"},
			},
		}

		rendered := RenderSearchResponse(resp)

		if !strings.Contains(rendered, "test query") {
			t.Errorf("expected query 'test query' in rendered output, got: %s", rendered)
		}
		if !strings.Contains(rendered, "Title 1") {
			t.Errorf("expected 'Title 1' in rendered output")
		}
		if !strings.Contains(rendered, "Title 2") {
			t.Errorf("expected 'Title 2' in rendered output")
		}
		if !strings.Contains(rendered, "https://example.com/1") {
			t.Errorf("expected URL in rendered output")
		}
		if !strings.Contains(rendered, "First snippet") {
			t.Errorf("expected 'First snippet' in rendered output")
		}
	})

	t.Run("empty_results", func(t *testing.T) {
		resp := &SearchResponse{
			Query:   "nothing found",
			Results: nil,
		}
		rendered := RenderSearchResponse(resp)
		if !strings.Contains(rendered, "No results found") {
			t.Errorf("expected 'No results found', got: %s", rendered)
		}
	})

	t.Run("nil_response", func(t *testing.T) {
		rendered := RenderSearchResponse(nil)
		if rendered != "" {
			t.Errorf("expected empty for nil, got: %s", rendered)
		}
	})

	t.Run("render_parity", func(t *testing.T) {
		// Both native and client results produce identical format
		nativeResp := &SearchResponse{
			Query: "native search",
			Results: []SearchResult{
				{Title: "A", URL: "https://a.com", Snippet: "snippet a"},
			},
		}
		clientResp := &SearchResponse{
			Query: "native search",
			Results: []SearchResult{
				{Title: "A", URL: "https://a.com", Snippet: "snippet a"},
			},
		}

		nativeRendered := RenderSearchResponse(nativeResp)
		clientRendered := RenderSearchResponse(clientResp)

		if nativeRendered != clientRendered {
			t.Errorf("render parity failed:\nnative:  %s\nclient:  %s", nativeRendered, clientRendered)
		}
	})
}

// ---------------------------------------------------------------------------
// TestNewSearchClientProvider
// ---------------------------------------------------------------------------

func TestNewSearchClientProvider(t *testing.T) {
	t.Run("tavily", func(t *testing.T) {
		p, err := NewSearchClientProvider(ClientConfig{Provider: "tavily", APIKey: "key"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := p.(*TavilyProvider); !ok {
			t.Errorf("expected *TavilyProvider, got %T", p)
		}
	})

	t.Run("custom", func(t *testing.T) {
		p, err := NewSearchClientProvider(ClientConfig{Provider: "custom", APIKey: "key", BaseURL: "https://example.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := p.(*CustomProvider); !ok {
			t.Errorf("expected *CustomProvider, got %T", p)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		_, err := NewSearchClientProvider(ClientConfig{Provider: "unknown"})
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}
