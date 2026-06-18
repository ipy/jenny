package tool

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// stubNativeSearchRunner is a stub NativeSearchRunner for testing.
type stubNativeSearchRunner struct {
	resp *SearchResponse
	err  error
}

func (s *stubNativeSearchRunner) RunNativeSearch(ctx context.Context, query string) (*SearchResponse, error) {
	return s.resp, s.err
}

// stubSearchClientProvider is a stub SearchClientProvider for testing.
type stubSearchClientProvider struct {
	resp *SearchResponse
	err  error
}

func (s *stubSearchClientProvider) Search(ctx context.Context, query string) (*SearchResponse, error) {
	return s.resp, s.err
}

// stubProvider is a minimal api.Provider for testing SupportsNativeSearch gating.
type stubProvider struct {
	supportsNative bool
}

func (s *stubProvider) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	return nil, nil
}
func (s *stubProvider) SendMessageStream(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan api.StreamContentBlock, *api.StreamResult) {
	return nil, nil
}
func (s *stubProvider) Kind() api.ProviderKind      { return api.ProviderAnthropic }
func (s *stubProvider) SetProviderName(name string) {}
func (s *stubProvider) SupportsNativeSearch() bool  { return s.supportsNative }

// testConfig returns a default native config for tests.
func testConfig() *WebSearchConfig {
	cfg := DefaultWebSearchConfig()
	return &cfg
}

func newTestWebSearchTool() *WebSearchTool {
	return NewWebSearchTool(testConfig(), &stubNativeSearchRunner{
		resp: &SearchResponse{
			Query:   "test",
			Results: []SearchResult{{Title: "Result", URL: "https://example.com", Snippet: "Snippet"}},
		},
	}, &stubSearchClientProvider{
		resp: &SearchResponse{
			Query:   "test",
			Results: []SearchResult{{Title: "Fallback", URL: "https://fallback.com", Snippet: "Snippet"}},
		},
	})
}

func TestWebSearchTool_NameAndDescription(t *testing.T) {
	tool := newTestWebSearchTool()
	if tool.Name() != "web_search" {
		t.Errorf("expected Name() to be 'web_search', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}

	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["query"]; !ok {
		t.Error("schema should have 'query' property")
	}
	if _, ok := props["allowed_domains"]; !ok {
		t.Error("schema should have 'allowed_domains' property")
	}
	if _, ok := props["blocked_domains"]; !ok {
		t.Error("schema should have 'blocked_domains' property")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required should be a []string")
	}
	found := slices.Contains(required, "query")
	if !found {
		t.Error("'query' should be in required")
	}
}

func TestWebSearchTool_AC2_QueryMinLength(t *testing.T) {
	tool := newTestWebSearchTool()
	ctx := context.Background()

	// Empty query
	result, err := tool.Execute(ctx, map[string]any{"query": ""}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for empty query")
	}
	if !strings.Contains(result.Content, "at least 2") {
		t.Errorf("expected error mentioning 'at least 2', got: %s", result.Content)
	}

	// Single character query
	result, err = tool.Execute(ctx, map[string]any{"query": "a"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for single char query")
	}

	// Two character query - should pass
	result, err = tool.Execute(ctx, map[string]any{"query": "ab"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error for 2-char query, got: %s", result.Content)
	}
}

func TestWebSearchTool_AC3_MutualExclusion(t *testing.T) {
	tool := newTestWebSearchTool()
	ctx := context.Background()

	// Both allowed and blocked domains - should error
	result, err := tool.Execute(ctx, map[string]any{
		"query":           "test",
		"allowed_domains": []any{"example.com"},
		"blocked_domains": []any{"evil.com"},
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError when both allowed and blocked domains set")
	}
	if !strings.Contains(result.Content, "mutually exclusive") {
		t.Errorf("expected error mentioning 'mutually exclusive', got: %s", result.Content)
	}

	// Only allowed_domains - should pass
	result, err = tool.Execute(ctx, map[string]any{
		"query":           "test",
		"allowed_domains": []any{"example.com"},
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error with only allowed_domains, got: %s", result.Content)
	}

	// Only blocked_domains - should pass
	result, err = tool.Execute(ctx, map[string]any{
		"query":           "test",
		"blocked_domains": []any{"evil.com"},
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error with only blocked_domains, got: %s", result.Content)
	}
}

func TestWebSearchTool_AC1_Name(t *testing.T) {
	tool := newTestWebSearchTool()
	if tool.Name() != "web_search" {
		t.Errorf("expected 'web_search', got %q", tool.Name())
	}
}

// TestWebSearchTool_Fallback tests fallback behavior for various scenarios.
func TestWebSearchTool_Fallback(t *testing.T) {
	ctx := context.Background()

	t.Run("native_ok", func(t *testing.T) {
		tool := NewWebSearchTool(
			testConfig(),
			&stubNativeSearchRunner{
				resp: &SearchResponse{
					Query: "test",
					Results: []SearchResult{
						{Title: "Native", URL: "https://native.com", Snippet: "From native"},
					},
				},
			},
			nil,
		)
		tool.WithProvider(&stubProvider{supportsNative: true})
		result, err := tool.Execute(ctx, map[string]any{"query": "test search"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("expected success from native, got error: %s", result.Content)
		}
		if !strings.Contains(result.Content, "Native") {
			t.Errorf("expected 'Native' in result, got: %s", result.Content)
		}
	})

	t.Run("native_fail_fallback_to_client", func(t *testing.T) {
		tool := NewWebSearchTool(
			testConfig(),
			&stubNativeSearchRunner{
				err: fmt.Errorf("native search failure"),
			},
			&stubSearchClientProvider{
				resp: &SearchResponse{
					Query: "test",
					Results: []SearchResult{
						{Title: "Fallback", URL: "https://fallback.com", Snippet: "From client"},
					},
				},
			},
		)
		tool.WithProvider(&stubProvider{supportsNative: true})
		result, err := tool.Execute(ctx, map[string]any{"query": "test search"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("expected success from fallback, got error: %s", result.Content)
		}
		if !strings.Contains(result.Content, "Fallback") {
			t.Errorf("expected 'Fallback' in result, got: %s", result.Content)
		}
	})

	t.Run("native_unsupported", func(t *testing.T) {
		// No native runner at all
		tool := NewWebSearchTool(
			testConfig(),
			nil,
			&stubSearchClientProvider{
				resp: &SearchResponse{
					Query: "test",
					Results: []SearchResult{
						{Title: "ClientOnly", URL: "https://client.com", Snippet: "From client fallback"},
					},
				},
			},
		)
		result, err := tool.Execute(ctx, map[string]any{"query": "test search"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("expected success from client fallback, got error: %s", result.Content)
		}
		if !strings.Contains(result.Content, "ClientOnly") {
			t.Errorf("expected 'ClientOnly' in result, got: %s", result.Content)
		}
	})

	t.Run("force_client", func(t *testing.T) {
		cfg := &WebSearchConfig{
			Strategy:     StrategyClient,
			ClientConfig: ClientConfig{Provider: "tavily", APIKey: "key"},
		}
		tool := NewWebSearchTool(
			cfg,
			&stubNativeSearchRunner{
				resp: &SearchResponse{
					Query: "test",
					Results: []SearchResult{
						{Title: "ShouldNotUseNative", URL: "https://native.com", Snippet: "Bad"},
					},
				},
			},
			&stubSearchClientProvider{
				resp: &SearchResponse{
					Query: "test",
					Results: []SearchResult{
						{Title: "ClientDirect", URL: "https://client.com", Snippet: "Direct"},
					},
				},
			},
		)
		result, err := tool.Execute(ctx, map[string]any{"query": "test search"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Errorf("expected success from direct client, got error: %s", result.Content)
		}
		if !strings.Contains(result.Content, "ClientDirect") {
			t.Errorf("expected 'ClientDirect' in result, got: %s", result.Content)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &WebSearchConfig{Strategy: StrategyDisabled}
		tool := NewWebSearchTool(cfg, nil, nil)
		result, err := tool.Execute(ctx, map[string]any{"query": "test search"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for disabled strategy")
		}
		if !strings.Contains(result.Content, "web search disabled") {
			t.Errorf("expected 'web search disabled', got: %s", result.Content)
		}
	})
}

// TestWebSearchTool_ConfigDriven tests strategy selection based on config values.
func TestWebSearchTool_ConfigDriven(t *testing.T) {
	ctx := context.Background()
	nativeResult := &SearchResponse{
		Query:   "test",
		Results: []SearchResult{{Title: "Native", URL: "https://n.com", Snippet: "s"}},
	}
	clientResult := &SearchResponse{
		Query:   "test",
		Results: []SearchResult{{Title: "Client", URL: "https://c.com", Snippet: "s"}},
	}

	t.Run("native_strategy", func(t *testing.T) {
		cfg := &WebSearchConfig{Strategy: StrategyNative}
		tool := NewWebSearchTool(cfg,
			&stubNativeSearchRunner{resp: nativeResult},
			&stubSearchClientProvider{resp: clientResult},
		)
		tool.WithProvider(&stubProvider{supportsNative: true})
		result, _ := tool.Execute(ctx, map[string]any{"query": "test"}, "")
		if !strings.Contains(result.Content, "Native") {
			t.Errorf("expected native result, got: %s", result.Content)
		}
	})

	t.Run("client_strategy", func(t *testing.T) {
		cfg := &WebSearchConfig{Strategy: StrategyClient}
		tool := NewWebSearchTool(cfg,
			&stubNativeSearchRunner{resp: nativeResult},
			&stubSearchClientProvider{resp: clientResult},
		)
		result, _ := tool.Execute(ctx, map[string]any{"query": "test"}, "")
		if !strings.Contains(result.Content, "Client") {
			t.Errorf("expected client result, got: %s", result.Content)
		}
	})

	t.Run("disabled_strategy", func(t *testing.T) {
		cfg := &WebSearchConfig{Strategy: StrategyDisabled}
		tool := NewWebSearchTool(cfg, nil, nil)
		result, _ := tool.Execute(ctx, map[string]any{"query": "test"}, "")
		if !result.IsError || !strings.Contains(result.Content, "disabled") {
			t.Errorf("expected disabled error, got: %v", result)
		}
	})

	t.Run("client_strategy_no_provider", func(t *testing.T) {
		cfg := &WebSearchConfig{Strategy: StrategyClient}
		tool := NewWebSearchTool(cfg, nil, nil)
		result, _ := tool.Execute(ctx, map[string]any{"query": "test"}, "")
		if !result.IsError {
			t.Error("expected error when client strategy with no provider")
		}
	})

	t.Run("native_fallback_unavailable", func(t *testing.T) {
		cfg := testConfig()
		tool := NewWebSearchTool(cfg, nil, nil)
		result, _ := tool.Execute(ctx, map[string]any{"query": "test"}, "")
		if !result.IsError {
			t.Error("expected error when no native runner and no client fallback")
		}
		if !strings.Contains(result.Content, "unavailable") {
			t.Errorf("expected 'unavailable' in error, got: %s", result.Content)
		}
	})
}

// TestWebSearchTool_AC5_ServerErrorCodes tests that Execute() handles various inputs without crashing.
func TestWebSearchTool_AC5_ServerErrorCodes(t *testing.T) {
	tool := newTestWebSearchTool()
	ctx := context.Background()

	tests := []struct {
		name  string
		input map[string]any
	}{
		{"valid query", map[string]any{"query": "test search"}},
		{"query with special chars", map[string]any{"query": "test <script>alert('xss')</script>"}},
		{"empty allowed_domains", map[string]any{"query": "test", "allowed_domains": []any{}}},
		{"empty blocked_domains", map[string]any{"query": "test", "blocked_domains": []any{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.input, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			// Successful execution should not contain error_code in content
			if strings.Contains(result.Content, "error_code") {
				t.Errorf("expected no error_code in result, got: %s", result.Content)
			}
		})
	}
}
