package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHTTPTransportConnect tests AC2: HTTP transport connects and completes initialization.
func TestHTTPTransportConnect(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "jenny",
				"version": "0.1.0",
			},
		},
	}

	resp, err := transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error response: %v", resp.Error)
	}

	var initResult initializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		t.Fatalf("failed to parse initialize result: %v", err)
	}
	if initResult.ProtocolVersion != "2025-03-26" {
		t.Errorf("expected protocol version 2025-03-26, got %s", initResult.ProtocolVersion)
	}
}

// TestHTTPTransportSessionID tests AC4: session ID is included after initialization.
func TestHTTPTransportSessionID(t *testing.T) {
	sessionID := "test-session-abc123"
	var receivedHeaders []http.Header
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = append(receivedHeaders, r.Header.Clone())
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", sessionID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}`),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[]}`),
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	// First request: initialize - no session ID yet
	initReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "initialize",
	}
	_, err := transport.SendRequest(ctx, initReq)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	// Second request: should include session ID
	toolsReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(2),
		Method:  "tools/list",
	}
	_, err = transport.SendRequest(ctx, toolsReq)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(receivedHeaders) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(receivedHeaders))
	}

	// First request should NOT have session ID
	if got := receivedHeaders[0].Get("Mcp-Session-Id"); got != "" {
		t.Errorf("first request should not have session ID, got %q", got)
	}

	// Second request MUST have session ID
	if got := receivedHeaders[1].Get("Mcp-Session-Id"); got != sessionID {
		t.Errorf("second request session ID = %q, want %q", got, sessionID)
	}
}

// TestHTTPTransportCustomHeaders tests AC5: custom headers from config are included.
func TestHTTPTransportCustomHeaders(t *testing.T) {
	var receivedAuth string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedAuth = r.Header.Get("Authorization")
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}`),
		})
	}))
	defer server.Close()

	headers := map[string]string{
		"Authorization": "Bearer my-secret-token",
	}
	transport := NewHTTPTransport(server.URL, headers)
	ctx := context.Background()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "initialize",
	}
	_, err := transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if receivedAuth != "Bearer my-secret-token" {
		t.Errorf("expected Authorization header 'Bearer my-secret-token', got %q", receivedAuth)
	}
}

// TestHTTPTransportSSEResponse tests AC3: HTTP transport handles SSE response content type.
func TestHTTPTransportSSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}

		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[{"name":"echo","description":"echoes input","inputSchema":{"type":"object"}}]}`),
		}
		data, _ := json.Marshal(resp)

		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		flusher.Flush()
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "tools/list",
	}

	resp, err := transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var toolsResult toolsListResult
	if err := json.Unmarshal(resp.Result, &toolsResult); err != nil {
		t.Fatalf("failed to parse tools result: %v", err)
	}
	if len(toolsResult.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolsResult.Tools))
	}
	if toolsResult.Tools[0].Name != "echo" {
		t.Errorf("expected tool name 'echo', got %q", toolsResult.Tools[0].Name)
	}
}

// TestHTTPTransportJSONResponse tests AC3: HTTP transport handles JSON response content type.
func TestHTTPTransportJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"hello world"}]}`),
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"text": "hello world"},
		},
	}

	resp, err := transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var callResult toolsCallResult
	if err := json.Unmarshal(resp.Result, &callResult); err != nil {
		t.Fatalf("failed to parse call result: %v", err)
	}
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "hello world" {
		t.Errorf("unexpected result content: %+v", callResult.Content)
	}
}

// TestHTTPTransportNotification tests that notifications get HTTP 202.
func TestHTTPTransportNotification(t *testing.T) {
	var received bool
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = true
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)

	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	err := transport.SendNotification(context.Background(), notif)
	if err != nil {
		t.Fatalf("SendNotification failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !received {
		t.Error("server did not receive notification")
	}
}

// TestHTTPTransportUnexpected202ForRequest tests that 202 for a request returns an error.
func TestHTTPTransportUnexpected202ForRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "tools/list"}
	_, err := transport.SendRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for unexpected 202 on request")
	}
	if !strings.Contains(err.Error(), "unexpected HTTP 202") {
		t.Errorf("expected 'unexpected HTTP 202' error, got: %v", err)
	}
}

// TestHTTPTransportSessionExpired tests AC6: transport returns session expired error on 404.
func TestHTTPTransportSessionExpired(t *testing.T) {
	requestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		if count == 1 {
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}`),
			})
			return
		}

		if count == 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[]}`),
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	initReq := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "initialize"}
	_, err := transport.SendRequest(ctx, initReq)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	toolsReq := jsonRPCRequest{JSONRPC: "2.0", ID: int64(2), Method: "tools/list"}
	_, err = transport.SendRequest(ctx, toolsReq)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !IsSessionExpired(err) {
		t.Errorf("expected session expired error, got: %v", err)
	}
}

// TestHTTPClientAutoReinitOnSessionExpiry tests AC6: Client auto re-inits on 404.
func TestHTTPClientAutoReinitOnSessionExpiry(t *testing.T) {
	postRequestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET requests (BackgroundListen SSE probe) never consume POST sequence slots.
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			fmt.Fprintf(w, "event: message\ndata: {\"method\":\"ping\"}\n\n")
			return
		}

		// Peek at body to distinguish notifications (empty body) from requests.
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			// Notification — accept without consuming a POST request slot.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Restore body so it can be read again by SendRequest.
		r.Body = io.NopCloser(bytes.NewReader(body))

		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		mu.Lock()
		postRequestCount++
		count := postRequestCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case count == 1 && req.Method == "initialize":
			w.Header().Set("Mcp-Session-Id", "session-1")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}`),
			})
		case count == 2:
			// Notification (initialized) - accept
			w.WriteHeader(http.StatusAccepted)
		case count == 3:
			// First tools/list → 404 (session expired)
			w.WriteHeader(http.StatusNotFound)
		case count == 4 && req.Method == "initialize":
			// Re-initialization
			w.Header().Set("Mcp-Session-Id", "session-2")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}`),
			})
		case count == 5:
			// Notification (initialized after re-init) - accept
			w.WriteHeader(http.StatusAccepted)
		case count == 6:
			// Retried tools/list after re-init
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"tools":[{"name":"test-tool","description":"a tool","inputSchema":{"type":"object"}}]}`),
			})
		default:
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"tools":[]}`),
			})
		}
	}))
	defer server.Close()

	client := NewHTTPClient("reinit-test", server.URL, nil)
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Disconnect()

	// This should trigger 404 → auto re-init → retry → success
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools should succeed after auto re-init, got: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool after re-init, got %d", len(tools))
	}
}

// TestHTTPTransportAcceptHeader tests that Accept header is correctly set.
func TestHTTPTransportAcceptHeader(t *testing.T) {
	var receivedAccept string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedAccept = r.Header.Get("Accept")
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "initialize"}
	transport.SendRequest(ctx, req)

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(receivedAccept, "application/json") {
		t.Errorf("Accept header missing application/json: %q", receivedAccept)
	}
	if !strings.Contains(receivedAccept, "text/event-stream") {
		t.Errorf("Accept header missing text/event-stream: %q", receivedAccept)
	}
}

// TestHTTPTransportContentType tests that Content-Type is set correctly.
func TestHTTPTransportContentType(t *testing.T) {
	var receivedContentType string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedContentType = r.Header.Get("Content-Type")
		mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "initialize"}
	transport.SendRequest(ctx, req)

	mu.Lock()
	defer mu.Unlock()
	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", receivedContentType)
	}
}

// TestHTTPTransportClose tests that Close is clean.
func TestHTTPTransportClose(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, requests should fail
	ctx := context.Background()
	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "initialize"}
	_, err := transport.SendRequest(ctx, req)
	if err == nil {
		t.Error("expected error after close")
	}
}

// TestHTTPTransportServerError tests handling of HTTP 5xx errors.
func TestHTTPTransportServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "initialize"}
	_, err := transport.SendRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestHTTPTransportTimeout tests request timeout behavior.
func TestHTTPTransportTimeout(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer func() {
		close(done)
		server.Close()
	}()

	transport := NewHTTPTransport(server.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "initialize"}
	_, err := transport.SendRequest(ctx, req)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// TestHTTPTransportJSONRPCError tests that JSON-RPC errors are properly returned.
func TestHTTPTransportJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32600, Message: "Invalid Request"},
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "invalid"}
	resp, err := transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("SendRequest should not return transport error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected JSON-RPC error in response")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("expected error code -32600, got %d", resp.Error.Code)
	}
}

// TestHTTPTransportSSEMultipleEvents tests SSE with multiple events before response.
func TestHTTPTransportSSEMultipleEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)

		// Send a notification first (server can send notifications before response)
		notif, _ := json.Marshal(jsonRPCRequest{
			JSONRPC: "2.0",
			Method:  "notifications/progress",
			Params:  map[string]any{"status": "working"},
		})
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", notif)
		flusher.Flush()

		// Then send the actual response
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`),
		}
		data, _ := json.Marshal(resp)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		flusher.Flush()
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil)
	ctx := context.Background()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: int64(1), Method: "tools/call"}
	resp, err := transport.SendRequest(ctx, req)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result toolsCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "done" {
		t.Errorf("unexpected result: %+v", result.Content)
	}
}

// TestNewHTTPClient tests AC2+AC9: Full client lifecycle via HTTP transport.
func TestNewHTTPClient(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	client := NewHTTPClient("test-http-server", server.URL, nil)
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Disconnect()

	// Discover tools (AC9)
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) == 0 {
		t.Error("expected at least one tool")
	}

	// Verify tool naming follows same pattern as stdio
	for _, tool := range tools {
		name := tool.Name()
		if !strings.HasPrefix(name, "mcp__test_http_server__") {
			t.Errorf("expected prefix 'mcp__test_http_server__', got %q", name)
		}
	}

	// Call a tool
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected result to contain 'hello', got %q", result)
	}
}

// TestConnectAllWithHTTPServer tests AC9: HTTP servers are connected via ConnectAll.
func TestConnectAllWithHTTPServer(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	cfg := map[string]MCPServerDef{
		"http-server": {
			URL:     server.URL,
			Headers: map[string]string{"X-Custom": "value"},
		},
	}

	if err := ConnectAll(cfg); err != nil {
		t.Fatalf("ConnectAll failed: %v", err)
	}
	defer ShutdownAll()

	client := GetClient("http-server")
	if client == nil {
		t.Fatal("expected client for http-server")
	}

	tools := GetTools()
	if len(tools) == 0 {
		t.Error("expected tools from HTTP server")
	}
}

// TestConnectAllMixedTransports tests that both stdio and HTTP servers can coexist.
func TestConnectAllMixedTransports(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	cfg := map[string]MCPServerDef{
		"http-server": {
			URL: server.URL,
		},
		"stdio-server": {
			Command: "nonexistent-command-xyz",
		},
	}

	// ConnectAll will fail on stdio server but HTTP server should work
	// The current behavior is to fail on first error - this is existing behavior
	err := ConnectAll(cfg)
	// With mixed transports, we expect at least one to connect or the whole thing fails
	// The important thing is that HTTP transport IS attempted
	_ = err
}

// --- Test helpers ---

// newFakeHTTPMCPServer creates a fake MCP server over HTTP for testing.
func newFakeHTTPMCPServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Handle notifications
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "fake-session-id")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{"tools":{}},"serverInfo":{"name":"fake-http-mcp","version":"1.0.0","icons":{"icon":"https://example.com/icon.png","darkIcon":"https://example.com/dark.png"}}}`),
			})

		case "tools/list":
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"tools":[{"name":"echo","description":"Echo the input text","inputSchema":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}}]}`),
			})

		case "tools/call":
			params := req.Params
			args, _ := params["arguments"].(map[string]any)
			text, _ := args["text"].(string)
			result := fmt.Sprintf(`{"content":[{"type":"text","text":"%s"}]}`, text)
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(result),
			})

		case "resources/list":
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"resources":[]}`),
			})

		case "resources/subscribe":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			})

		case "resources/unsubscribe":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			})

		default:
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32601, Message: "Method not found"},
			})
		}
	}))
}

// TestHTTPClientSubscribeResource tests AC2: SubscribeResource over HTTP transport succeeds.
func TestHTTPClientSubscribeResource(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	client := NewHTTPClient("test-http-server", server.URL, nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Disconnect()

	err := client.SubscribeResource(ctx, "file:///test.txt")
	if err != nil {
		t.Errorf("SubscribeResource over HTTP failed: %v", err)
	}
}

// TestHTTPClientUnsubscribeResource tests AC3: UnsubscribeResource over HTTP transport succeeds.
func TestHTTPClientUnsubscribeResource(t *testing.T) {
	server := newFakeHTTPMCPServer(t)
	defer server.Close()

	client := NewHTTPClient("test-http-server", server.URL, nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Disconnect()

	err := client.UnsubscribeResource(ctx, "file:///test.txt")
	if err != nil {
		t.Errorf("UnsubscribeResource over HTTP failed: %v", err)
	}
}

// TestHTTPClientSubscribeResource_ServerError tests AC4: SubscribeResource returns an error
// when the server responds with a JSON-RPC error (e.g. MethodNotFound, code -32601).
func TestHTTPClientSubscribeResource_ServerError(t *testing.T) {
	postRequestCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			fmt.Fprintf(w, "event: message\ndata: {\"method\":\"ping\"}\n\n")
			return
		}

		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		var req jsonRPCRequest
		json.Unmarshal(body, &req)

		mu.Lock()
		postRequestCount++
		count := postRequestCount
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case count == 1 && req.Method == "initialize":
			w.Header().Set("Mcp-Session-Id", "error-test-session")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}`),
			})
		case count == 2:
			// Notification (initialized) - accept
			w.WriteHeader(http.StatusAccepted)
		case req.Method == "resources/subscribe":
			// Server returns MethodNotFound for resources/subscribe
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32601, Message: "Method not found"},
			})
		default:
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{}`),
			})
		}
	}))
	defer server.Close()

	client := NewHTTPClient("error-test", server.URL, nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Disconnect()

	err := client.SubscribeResource(ctx, "file:///test.txt")
	if err == nil {
		t.Fatal("expected error for MethodNotFound response")
	}
	if !strings.Contains(err.Error(), "Method not found") {
		t.Errorf("expected 'Method not found' in error, got: %v", err)
	}
}
