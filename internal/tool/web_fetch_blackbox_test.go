package tool_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

func TestWebFetch_AC1_10MBBodyLimit(t *testing.T) {
	// Start a server that returns > 10 MB of data.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 11 MB of 'a' characters.
		data := strings.Repeat("a", 11*1024*1024)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(data))
	}))
	defer srv.Close()

	fetchTool := tool.NewWebFetchTool().WithSkipBlocklist()
	ctx := context.Background()

	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for response exceeding 10MB limit")
	}
	if !strings.Contains(result.Content, "10 MB") && !strings.Contains(result.Content, "limit") {
		t.Errorf("expected error mentioning size limit, got: %s", result.Content)
	}
}

func TestWebFetch_AC2_100KMarkdownCap(t *testing.T) {
	// Start a server that returns HTML that will produce > 100K chars of markdown.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Generate enough paragraphs to exceed the 100K cap.
		var sb strings.Builder
		sb.WriteString("<html><body>")
		for i := range 3000 {
			sb.WriteString(fmt.Sprintf("<p>This is paragraph number %d with enough content to make each paragraph substantial in length for the test.</p>", i))
		}
		sb.WriteString("</body></html>")
		w.Write([]byte(sb.String()))
	}))
	defer srv.Close()

	fetchTool := tool.NewWebFetchTool().WithSkipBlocklist()
	ctx := context.Background()

	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(result.Content) > 110000 { // allow small overhead
		t.Errorf("expected markdown to be capped near 100K chars, got %d chars", len(result.Content))
	}
	if !result.Truncated {
		t.Error("expected Truncated flag to be set")
	}
}

func TestWebFetch_AC3_BlocklistLocalhost(t *testing.T) {
	fetchTool := tool.NewWebFetchTool()
	ctx := context.Background()

	// This should be blocked before any HTTP request is made — no server needed.
	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": "http://localhost:9999/test",
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

func TestWebFetch_AC4_CrossHostRedirect(t *testing.T) {
	// Start a server that redirects to a different host.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://other.example.com/page")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	fetchTool := tool.NewWebFetchTool().WithSkipBlocklist()
	ctx := context.Background()

	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("cross-host redirect should not be an error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "redirect") && !strings.Contains(result.Content, "Re-fetch") {
		t.Errorf("expected re-fetch instruction, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "http://other.example.com/page") {
		t.Errorf("expected target URL in response, got: %s", result.Content)
	}
}

func TestWebFetch_AC4_SameHostRedirect(t *testing.T) {
	// Start a server that redirects to the same host.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body><h1>Final Destination</h1></body></html>"))
			return
		}
		w.Header().Set("Location", "/final")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	fetchTool := tool.NewWebFetchTool().WithSkipBlocklist()
	ctx := context.Background()

	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("same-host redirect should not be an error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Final Destination") {
		t.Errorf("expected final page content, got: %s", result.Content)
	}
}

func TestWebFetch_AC5_CredentialsRejected(t *testing.T) {
	fetchTool := tool.NewWebFetchTool()
	ctx := context.Background()

	// No server needed — credentials are rejected pre-flight.
	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": "http://user:pass@example.com/data",
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for URL with embedded credentials")
	}
	if !strings.Contains(result.Content, "credentials") {
		t.Errorf("expected error mentioning 'credentials', got: %s", result.Content)
	}
}

func TestWebFetch_BinarySave(t *testing.T) {
	// Start a server returning binary content.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // minimal PNG header
	}))
	defer srv.Close()

	fetchTool := tool.NewWebFetchTool().WithSkipBlocklist()
	ctx := context.Background()

	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.HasSuffix(result.Content, ".bin") && !strings.Contains(result.Content, "Saved to") {
		t.Errorf("expected saved path in result, got: %s", result.Content)
	}
}

func TestWebFetch_Timeout_60s(t *testing.T) {
	// Basic connectivity test — should complete well within the 60s timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><p>Hello</p></body></html>"))
	}))
	defer srv.Close()

	fetchTool := tool.NewWebFetchTool().WithSkipBlocklist()
	ctx := context.Background()

	result, err := fetchTool.Execute(ctx, map[string]any{
		"url": srv.URL,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello") {
		t.Errorf("expected content, got: %s", result.Content)
	}
}
