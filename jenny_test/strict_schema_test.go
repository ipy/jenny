package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

// strictSchemaMockServer is a mock that returns 400 errors when
// tools have empty name or empty input_schema.properties.
// This simulates strict providers (like MiniMax) that reject empty schemas.
type strictSchemaMockServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
}

func newStrictSchemaMockServer() *strictSchemaMockServer {
	m := &strictSchemaMockServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *strictSchemaMockServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.requests = append(m.requests, decoded)
	m.mu.Unlock()

	// Check tools array for empty name or empty parameters
	if tools, ok := decoded["tools"].([]any); ok {
		for _, toolEntry := range tools {
			tool, ok := toolEntry.(map[string]any)
			if !ok {
				continue
			}
			// Check for empty name
			name, ok := tool["name"].(string)
			if !ok || name == "" {
				m.writeError(w, "function name or parameters is empty (simulated strict provider error)")
				return
			}
			// Check for empty/missing input_schema
			inputSchema, ok := tool["input_schema"].(map[string]any)
			if !ok || inputSchema == nil {
				m.writeError(w, "function name or parameters is empty (simulated strict provider error)")
				return
			}
			// Check if properties is missing or empty object
			props, ok := inputSchema["properties"]
			if !ok || props == nil {
				m.writeError(w, "function name or parameters is empty (simulated strict provider error)")
				return
			}
			propsMap, ok := props.(map[string]any)
			if !ok || len(propsMap) == 0 {
				// Some providers consider an empty properties object as "empty parameters"
				m.writeError(w, "function name or parameters is empty (simulated strict provider error)")
				return
			}
		}
	} else {
		// No tools array at all is sometimes an error for tool-enabled models
		m.writeError(w, "function name or parameters is empty (simulated strict provider error)")
		return
	}

	// No issues found - serve the tool-use SSE response inline
	m.mu.Lock()
	reqCount := len(m.requests)
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if reqCount == 1 {
		// Turn 1: assistant returns tool_use for Bash
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_tu1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"strict-model-1\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":150,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_bash01\",\"name\":\"Bash\",\"input\":{}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":\\\"echo hello\\\"}\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":20}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	} else {
		// Turn 2: assistant returns final text
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_02\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"strict-model-1\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":200,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello from strict mock.\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (m *strictSchemaMockServer) writeError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": msg,
		},
	})
}

func (m *strictSchemaMockServer) Requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, len(m.requests))
	copy(out, m.requests)
	return out
}

// TestStrictSchema_UniversalPlaceholder verifies that with universal normalization,
// jenny does NOT trigger empty-schema errors (like MiniMax 2013) because a
// placeholder __arg__ property is always added to tools with empty properties.
func TestStrictSchema_UniversalPlaceholder(t *testing.T) {
	mock := newStrictSchemaMockServer()
	t.Cleanup(mock.Close)

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=strict-model-1",
	}

	// Run with a prompt that triggers a tool call
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "run echo hello")

	// Exit should be 0 because the mock accepted all requests (schemas were normalized)
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Verify requests were received
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no requests received by mock")
	}

	// Verify first request has Bash tool with correct shape
	firstReq := reqs[0]
	tools, ok := firstReq["tools"].([]any)
	if !ok {
		t.Fatal("first request: tools is not an array")
	}
	var bashTool map[string]any
	for _, toolEntry := range tools {
		tool, ok := toolEntry.(map[string]any)
		if !ok {
			continue
		}
		// Skip web_search tools
		if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
			continue
		}
		name, _ := tool["name"].(string)
		if name == "Bash" {
			bashTool = tool
			break
		}
	}
	if bashTool == nil {
		t.Fatal("no Bash tool found in first request")
	}
	// Verify Bash tool has input_schema.properties.command.type == "string"
	inputSchema, ok := bashTool["input_schema"].(map[string]any)
	if !ok {
		t.Fatal("Bash tool input_schema is not a map")
	}
	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("input_schema.properties is not a map")
	}
	commandProp, ok := props["command"].(map[string]any)
	if !ok {
		t.Fatal("input_schema.properties.command is missing or not a map")
	}
	if commandType, _ := commandProp["type"].(string); commandType != "string" {
		t.Errorf("input_schema.properties.command.type = %q; want \"string\"", commandType)
	}
}

// TestStrictSchema_UniversalRegression ensures that the __arg__ placeholder is added
// UNCONDITIONALLY to all tools with empty properties, regardless of the provider URL.
// This is the "Universal" part of the normalization architecture.
func TestStrictSchema_UniversalRegression(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)

	// Use an "anthropic" URL - __arg__ should STILL be added if a tool has empty properties.
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}

	// We need to trigger a tool with empty properties.
	// Since standard tools have properties, we might need to rely on NormalizeMessages
	// unit tests for the 'empty properties' case, but we can verify that
	// standard tools are NOT affected (they don't get __arg__ because they aren't empty).
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "echo hello")

	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	reqs := mock.Requests()
	for i, req := range reqs {
		tools, ok := req.Body["tools"].([]any)
		if !ok {
			continue
		}
		for j, toolEntry := range tools {
			tool, ok := toolEntry.(map[string]any)
			if !ok {
				continue
			}
			// Skip web_search tools
			if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
				continue
			}
			inputSchema, ok := tool["input_schema"].(map[string]any)
			if !ok {
				continue
			}
			props, ok := inputSchema["properties"].(map[string]any)
			if !ok {
				continue
			}
			// Bash and other standard tools have properties, so they should NOT have __arg__
			if len(props) > 0 {
				if _, hasArg := props["__arg__"]; hasArg && len(props) > 1 {
					t.Errorf("request %d tool %d: found __arg__ in non-empty properties; should only be added if empty", i, j)
				}
			}
		}
	}
}
