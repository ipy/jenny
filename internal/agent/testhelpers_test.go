package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/testutil"
	"github.com/ipy/jenny/internal/testutil/mockapi"
	"github.com/ipy/jenny/internal/tool"
)

// captureStdout delegates to testutil.CaptureStdout for stdout capture.
var captureStdout = testutil.CaptureStdout

// sseLine delegates to testutil.SSELine for SSE event formatting.
var sseLine = testutil.SSELine

func makeMockStreamServerWithPartialEvents(t *testing.T) *httptest.Server {
	t.Helper()
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req api.AnthropicRequest
		json.Unmarshal(bodyBytes, &req)

		if !req.Stream {
			resp := api.AnthropicResponse{
				Type: "message",
				Role: "assistant",
				Content: []api.AnthropicContentBlock{
					{Type: "text", Text: "Fallback response"},
				},
				Model:      "test-model",
				StopReason: "end_turn",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	return ms.Server
}

func makeMockStreamServerWithCacheTokens(t *testing.T) *httptest.Server {
	t.Helper()
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req api.AnthropicRequest
		json.Unmarshal(bodyBytes, &req)

		if !req.Stream {
			resp := api.AnthropicResponse{
				Type: "message",
				Role: "assistant",
				Content: []api.AnthropicContentBlock{
					{Type: "text", Text: "Fallback response with cache tokens"},
				},
				Model:      "test-model",
				StopReason: "end_turn",
				Usage: api.AnthropicUsage{
					InputTokens:              5,
					OutputTokens:             2,
					CacheReadInputTokens:     3,
					CacheCreationInputTokens: 1,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		// SSE events with all four token types in message_delta usage
		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from stream"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			// message_delta with all four token types including cache tokens (AC1, AC4)
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			fmt.Fprint(w, e)
			flusher.Flush()
		}
	})
	return ms.Server
}

// makeMockStreamServerWithEvents creates a mock SSE server with explicit event slice.
func makeMockStreamServerWithEvents(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	ms := mockapi.NewMockServer()

	// Serve POST /v1/messages (SDK streaming endpoint)
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req api.AnthropicRequest
		json.Unmarshal(bodyBytes, &req)

		if !req.Stream {
			// Non-streaming response for summary agent or other calls
			resp := api.AnthropicResponse{
				Type: "message",
				Role: "assistant",
				Content: []api.AnthropicContentBlock{
					{Type: "text", Text: "Summary of the conversation"},
				},
				Model:      "test-model",
				StopReason: "end_turn",
				Usage: api.AnthropicUsage{
					InputTokens:  100,
					OutputTokens: 10,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		writeSSEEvents(w, events)
	})

	// Also serve GET / so that TestHelpers (which does http.Get(server.URL)) works.
	ms.SetPathHandler("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, events)
	})

	return ms.Server
}

// writeSSEEvents writes SSE headers and the given events to the response.
func writeSSEEvents(w http.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	flusher.Flush()

	for _, e := range events {
		io.WriteString(w, e)
		flusher.Flush()
	}
}

// TestHelpers verifies that the SSE mock server helpers produce valid servers.
func TestHelpers(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message"}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	server := makeMockStreamServerWithEvents(t, events)
	defer server.Close()

	// Verify server is reachable and returns SSE headers
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type: text/event-stream, got: %s", resp.Header.Get("Content-Type"))
	}
}

// parseAssistantEvents returns all parsed StreamMessage envelopes of type
// "assistant" found in the NDJSON output, preserving original order.
func parseAssistantEvents(t *testing.T, ndjson string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(ndjson, "\n") {
		if !strings.Contains(line, `"type":"assistant"`) {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal assistant line: %v\nline: %s", err, line)
		}
		out = append(out, msg)
	}
	return out
}

// parseNDJSONLines parses output into a slice of map[string]any for each line.
func parseNDJSONLines(t *testing.T, output string) []map[string]any {
	var result []map[string]any
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Logf("Warning: failed to parse JSON line: %q, error: %v", line, err)
			continue
		}
		result = append(result, m)
	}
	return result
}

// hasTextWith reports whether any element of content is a text block whose
// text matches want.
func hasTextWith(content []any, want string) bool {
	for _, item := range content {
		b, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if b["type"] == "text" && b["text"] == want {
			return true
		}
	}
	return false
}

// hasToolUseWithID reports whether any element of content is a tool_use block
// with id == want.
func hasToolUseWithID(content []any, want string) bool {
	for _, item := range content {
		b, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if b["type"] == "tool_use" && b["id"] == want {
			return true
		}
	}
	return false
}

// fastClient returns an API client for testing. If ANTHROPIC_BASE_URL is not
// already set via t.Setenv, it sets a safe default so the client never
// accidentally hits a real API endpoint. Tests that need a working mock
// server should set t.Setenv("ANTHROPIC_BASE_URL", server.URL) BEFORE calling
// fastClient(t), or use newMockClient(t) which creates both server and client.
func fastClient(t *testing.T) api.Requester {
	t.Helper()
	if os.Getenv("ANTHROPIC_BASE_URL") == "" {
		t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:0")
	}
	if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-token")
	}
	client, _ := api.NewClient()
	client.SetRetryConfig(api.RetryConfig{
		MaxRetries:    0,
		Max529Retries: 0,
	})
	return client
}

// newMockClient creates an API client backed by a mock server that responds
// to both streaming and non-streaming Anthropic API requests. The caller
// should defer close the returned cleanup function.
//
// This replaces fastClient(t) for integration tests that need a working API
// client without hitting real endpoints.
func newMockClient(t *testing.T) (api.Requester, func()) {
	t.Helper()

	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req api.AnthropicRequest
		json.Unmarshal(bodyBytes, &req)

		if !req.Stream {
			resp := api.AnthropicResponse{
				Type: "message",
				Role: "assistant",
				Content: []api.AnthropicContentBlock{
					{Type: "text", Text: "mock response"},
				},
				Model:      "test-model",
				StopReason: "end_turn",
				Usage: api.AnthropicUsage{
					InputTokens:  10,
					OutputTokens: 5,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"mock response"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		writeSSEEvents(w, events)
	})

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-token")

	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("newMockClient: failed to create client: %v", err)
	}
	client.SetRetryConfig(api.RetryConfig{
		MaxRetries:    0,
		Max529Retries: 0,
	})

	return client, ms.Close
}

// mustNewQueryEngine creates a QueryEngine for testing, panicking on error.
// All test callers use WithClient so the error path is never reached.
func mustNewQueryEngine(cfg *StreamConfig, tools []tool.Tool, model string, opts ...QueryEngineOption) *QueryEngine {
	e, err := NewQueryEngine(cfg, tools, model, opts...)
	if err != nil {
		panic(fmt.Sprintf("mustNewQueryEngine: %v", err))
	}
	return e
}
