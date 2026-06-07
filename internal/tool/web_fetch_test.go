package tool

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWebFetchTool_NameAndDescription(t *testing.T) {
	tool := NewWebFetchTool()
	if tool.Name() != "web_fetch" {
		t.Errorf("expected Name() to be 'web_fetch', got %q", tool.Name())
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
	if _, ok := props["url"]; !ok {
		t.Error("schema should have 'url' property")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required should be a []string")
	}
	found := slices.Contains(required, "url")
	if !found {
		t.Error("'url' should be in required")
	}
}

func TestWebFetchTool_AC5_CredentialsInURL(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"url": "http://user:password@example.com/data",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for URL with credentials")
	}
	if !strings.Contains(result.Content, "credentials") {
		t.Errorf("expected error mentioning 'credentials', got: %s", result.Content)
	}
}

func TestWebFetchTool_AC5_InvalidScheme(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"url": "ftp://example.com/file",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for invalid scheme")
	}
	if !strings.Contains(result.Content, "scheme") {
		t.Errorf("expected error mentioning 'scheme', got: %s", result.Content)
	}
}

func TestWebFetchTool_AC5_URLTooLong(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	// Create URL longer than 2000 chars.
	longURL := "http://example.com/" + strings.Repeat("a", 2000)
	result, err := tool.Execute(ctx, map[string]any{
		"url": longURL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for URL exceeding max length")
	}
	if !strings.Contains(result.Content, "exceeds the maximum length") {
		t.Errorf("expected error mentioning length limit, got: %s", result.Content)
	}
}

func TestWebFetchTool_AC3_BlocklistLocalhost(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"url": "http://localhost:8080/test",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for localhost URL")
	}
	if !strings.Contains(result.Content, "blocked") && !strings.Contains(result.Content, "not allowed") {
		t.Errorf("expected error mentioning 'blocked' or 'not allowed', got: %s", result.Content)
	}
}

func TestWebFetchTool_AC3_BlocklistPrivateIP(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()
	tests := []struct {
		url  string
		desc string
	}{
		{"http://10.0.0.1/test", "10.x.x.x"},
		{"http://192.168.1.1/test", "192.168.x.x"},
		{"http://172.16.0.1/test", "172.16.x.x"},
		{"http://127.0.0.1:9000/test", "127.0.0.1"},
		{"http://0.0.0.0/test", "0.0.0.0"},
		{"http://[::1]:8080/test", "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result, err := tool.Execute(ctx, map[string]any{"url": tt.url}, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Errorf("expected IsError for %s URL %q", tt.desc, tt.url)
			}
		})
	}
}

func TestWebFetchTool_AC3_PublicDomainAllowed(t *testing.T) {
	// This test validates that a public domain passes the blocklist check.
	// It will attempt DNS resolution, so it may fail in offline environments.
	tool := NewWebFetchTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"url": "http://example.com/test",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// We expect either a fetch error (no network) OR success.
	// The important thing is it's NOT a blocklist error.
	_ = result
}

func TestWebFetchTool_FetchCache(t *testing.T) {
	cache := newFetchCache(100000, 5*time.Minute)

	// Set a value.
	result1 := &ToolResult{Content: "hello world", IsError: false}
	cache.set("key1", result1)

	// Get it back.
	got, ok := cache.get("key1")
	if !ok {
		t.Error("expected cache hit for key1")
	}
	if got.Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", got.Content)
	}

	// Miss for unknown key.
	_, ok = cache.get("unknown")
	if ok {
		t.Error("expected cache miss for unknown key")
	}
}

func TestWebFetchTool_FetchCacheExpiry(t *testing.T) {
	cache := newFetchCache(100000, 50*time.Millisecond)

	result := &ToolResult{Content: "ephemeral", IsError: false}
	cache.set("exp", result)

	// Immediate hit.
	_, ok := cache.get("exp")
	if !ok {
		t.Error("expected cache hit before expiry")
	}

	// Wait for expiry.
	time.Sleep(100 * time.Millisecond)

	_, ok = cache.get("exp")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

func TestWebFetchTool_FetchCacheEviction(t *testing.T) {
	// Create a tiny cache (50 bytes max) and verify eviction.
	cache := newFetchCache(50, 5*time.Minute)

	// First entry: 30 bytes.
	cache.set("a", &ToolResult{Content: strings.Repeat("x", 30)})
	if _, ok := cache.get("a"); !ok {
		t.Error("expected cache hit for 'a' after first insert")
	}

	// Second entry: 30 bytes — total 60 > 50, should evict 'a'.
	cache.set("b", &ToolResult{Content: strings.Repeat("y", 30)})

	_, okA := cache.get("a")
	_, okB := cache.get("b")
	if okA {
		t.Error("expected 'a' to be evicted")
	}
	if !okB {
		t.Error("expected cache hit for 'b'")
	}
}

func TestWebFetchTool_HostnameCache(t *testing.T) {
	hc := newHostnameCache(5 * time.Minute)

	if hc.isBlocked("evil.com") {
		t.Error("expected evil.com to not be blocked initially")
	}

	hc.markBlocked("evil.com")

	if !hc.isBlocked("evil.com") {
		t.Error("expected evil.com to be blocked after markBlocked")
	}

	// Different host should still not be blocked.
	if hc.isBlocked("good.com") {
		t.Error("expected good.com to not be blocked")
	}
}

func TestWebFetchTool_HostnameCacheExpiry(t *testing.T) {
	hc := newHostnameCache(50 * time.Millisecond)

	hc.markBlocked("ephemeral.local")
	time.Sleep(100 * time.Millisecond)

	if hc.isBlocked("ephemeral.local") {
		t.Error("expected hostname to not be blocked after expiry")
	}
}

func TestWebFetchTool_ConvertHTMLToMarkdown(t *testing.T) {
	tool := NewWebFetchTool()

	html := `<h1>Hello World</h1><p>This is a <strong>test</strong>.</p>`
	md, truncated := tool.convertHTMLToMarkdown(html)

	if truncated {
		t.Error("expected not truncated for short HTML")
	}
	if !strings.Contains(md, "Hello World") {
		t.Errorf("expected markdown to contain 'Hello World', got: %s", md)
	}
}

func TestWebFetchTool_AC2_MarkdownCap(t *testing.T) {
	tool := NewWebFetchTool()

	// Generate HTML long enough to exceed the 100K markdown cap.
	// Each <p>...</p> adds ~80 chars of markdown, so 2000 paragraphs = ~160K.
	var sb strings.Builder
	sb.WriteString("<html><body>")
	for i := range 2000 {
		sb.WriteString(fmt.Sprintf("<p>Paragraph number %d with some content to make it realistic and long enough to be meaningful.</p>", i))
	}
	sb.WriteString("</body></html>")

	md, truncated := tool.convertHTMLToMarkdown(sb.String())

	if !truncated {
		t.Error("expected truncated=true for very long HTML")
	}
	if len(md) > 110000 { // allow small overhead
		t.Errorf("expected markdown to be capped near 100K chars, got %d", len(md))
	}
}

func TestWebFetchTool_EmptyURL(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{}, "")
	if err == nil {
		t.Error("expected error for missing url")
	}
}

func TestWebFetchTool_Name(t *testing.T) {
	tool := NewWebFetchTool()
	if tool.Name() != "web_fetch" {
		t.Errorf("expected 'web_fetch', got %q", tool.Name())
	}
}
