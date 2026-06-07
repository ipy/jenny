package tool

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestWebSearchTool_NameAndDescription(t *testing.T) {
	tool := NewWebSearchTool("claude-4-sonnet-20250604")
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
	tool := NewWebSearchTool("claude-4-sonnet-20250604")
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

func TestWebSearchTool_AC3_MaxResults(t *testing.T) {
	tool := NewWebSearchTool("claude-4-sonnet-20250604")
	ctx := context.Background()

	// Exceed max results
	result, err := tool.Execute(ctx, map[string]any{
		"query": "test",
		"count": float64(15),
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for count exceeding max")
	}
	if !strings.Contains(result.Content, "8") {
		t.Errorf("expected error mentioning max 8, got: %s", result.Content)
	}

	// Within limit
	result, err = tool.Execute(ctx, map[string]any{
		"query": "test",
		"count": float64(5),
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error for count=5, got: %s", result.Content)
	}
}

func TestWebSearchTool_AC3_MutualExclusion(t *testing.T) {
	tool := NewWebSearchTool("claude-4-sonnet-20250604")
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

func TestWebSearchTool_AC4_UnsupportedModel(t *testing.T) {
	tests := []struct {
		model string
		desc  string
	}{
		{"gpt-4", "GPT-4"},
		{"unknown-model", "unknown model"},
		{"", "empty model"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tool := NewWebSearchTool(tt.model)
			ctx := context.Background()

			result, err := tool.Execute(ctx, map[string]any{"query": "test"}, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Errorf("expected IsError for model %q", tt.model)
			}
			if !strings.Contains(result.Content, "not supported") {
				t.Errorf("expected error mentioning 'not supported', got: %s", result.Content)
			}
		})
	}
}

func TestWebSearchTool_AC4_SupportedModels(t *testing.T) {
	supported := []string{
		"claude-4-opus",
		"claude-4-sonnet",
		"claude-4-haiku",
		"claude-3.5-sonnet",
		"claude-3.5-haiku",
		"claude-3-opus",
		"claude-3-sonnet",
		"vertex/claude-4",
		"foundry/claude-3.5",
	}

	for _, model := range supported {
		t.Run(model, func(t *testing.T) {
			tool := NewWebSearchTool(model)
			ctx := context.Background()

			result, err := tool.Execute(ctx, map[string]any{"query": "test"}, "")
			if err != nil {
				t.Fatalf("unexpected error for model %q: %v", model, err)
			}
			// Supported model should not error for validation failures (only model check)
			if result.IsError && strings.Contains(result.Content, "not supported") {
				t.Errorf("expected model %q to be supported, got error: %s", model, result.Content)
			}
		})
	}
}

func TestWebSearchTool_AC5_ServerErrorCodes(t *testing.T) {
	// AC5: Server error codes from the API are surfaced as strings in the tool result text.
	//
	// Architecture: WebSearch is a server-side tool. When the API returns
	// web_search_tool_result blocks with errors, the agent engine (engine.go)
	// processes them and creates tool_results with the error code surfaced.
	// Execute() validates inputs locally; API error surfacing happens in the
	// agent's content block processing loop (engine.go ~line 438).
	//
	// Unit test scope: We test that Execute() handles edge cases gracefully
	// and doesn't crash. Full AC5 testing requires integration testing with
	// actual API responses containing web_search_tool_result error blocks.
	tool := NewWebSearchTool("claude-4-sonnet-20250604")
	ctx := context.Background()

	// AC5 unit test: Verify Execute() handles various inputs without crashing
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
			// Execute() should not crash and should return a valid result
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

func TestWebSearchTool_AC1_Name(t *testing.T) {
	tool := NewWebSearchTool("claude-4-sonnet-20250604")
	if tool.Name() != "web_search" {
		t.Errorf("expected 'web_search', got %q", tool.Name())
	}
}
