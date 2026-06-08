package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/session"
)

func sseLine(event, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
}

func makeMockStreamServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume and discard request body
		io.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		// Send a complete streaming response (text block, end_turn)
		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from stream"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
}

// TestAC4_StreamRequestStartEmitted verifies that RunStream emits
// stream_request_start before each API iteration when streaming is enabled.
func TestAC4_StreamRequestStartEmitted(t *testing.T) {
	server := makeMockStreamServer(t)
	defer server.Close()

	// Redirect SDK to our mock server
	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key-00000")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	// Redirect stdout to a pipe
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Write end must be closed before reading, so RunStream must complete first
	errCh := make(chan error, 1)
	go func() {
		// Use a temp dir so session persistence doesn't interfere
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}

		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	// Wait for RunStream to finish
	err := <-errCh

	// Close write end so we can read all output
	w.Close()
	os.Stdout = oldStdout

	// Read all captured stdout
	var outputBuf bytes.Buffer
	if _, err := io.Copy(&outputBuf, r); err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)

	// ----- AC4 verification -----
	if !strings.Contains(output, "stream_request_start") {
		t.Error("AC4 FAIL: stream_request_start not found in stdout output when cfg.Enabled=true")
	} else {
		t.Log("AC4 PASS: stream_request_start emitted in stdout")
	}

	// Also verify it appears on its own line (valid NDJSON)
	lines := strings.Split(output, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "stream_request_start") {
			found = true
			if !strings.HasPrefix(line, `{"type":"stream_request_start"`) {
				t.Errorf("AC4 FAIL: stream_request_start line is not valid NDJSON: %q", line)
			}
		}
	}
	if !found && !t.Failed() {
		t.Error("AC4 FAIL: stream_request_start not found in any output line")
	}
}

// TestAC4_NoStreamRequestStartWhenDisabled verifies that stream_request_start
// is NOT emitted when streaming is disabled.
func TestAC4_NoStreamRequestStartWhenDisabled(t *testing.T) {
	server := makeMockStreamServer(t)
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key-00000")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        false, // Streaming disabled
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream (disabled) completed with: %v", err)

	if strings.Contains(output, "stream_request_start") {
		t.Error("AC4 FAIL: stream_request_start found in output when cfg.Enabled=false")
	} else {
		t.Log("AC4 PASS: no stream_request_start when disabled")
	}
}

// makeMockStreamServerWithPartialEvents creates an httptest server that sends
// a canned SSE event sequence including partial message events.
func makeMockStreamServerWithPartialEvents(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		// Send a streaming sequence with message_start, content_block_delta, message_stop
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
	}))
}

// TestStreamEvent_EmittedWhenFlagOn verifies that stream_event wire shape is
// emitted when --include-partial-messages flag is enabled (IncludePartial=true).
func TestStreamEvent_EmittedWhenFlagOn(t *testing.T) {
	// t.Setenv for iter91 hygiene
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	server := makeMockStreamServerWithPartialEvents(t)
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        true,
			IncludePartial: true,
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)
	t.Logf("Output: %s", output)

	// AC1: At least one line satisfies type == "stream_event"
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	hasStreamEvent := false
	eventTypes := make(map[string]int)
	for _, line := range lines {
		if strings.Contains(line, `"type":"stream_event"`) {
			hasStreamEvent = true
			// Extract event type
			for _, l := range lines {
				if strings.Contains(l, `"type":"stream_event"`) {
					// Try to parse and extract event.type
					var wrapper struct {
						Type  string `json:"type"`
						Event struct {
							Type string `json:"type"`
						} `json:"event"`
					}
					if json.Unmarshal([]byte(l), &wrapper) == nil {
						eventTypes[wrapper.Event.Type]++
					}
				}
			}
		}
	}

	if !hasStreamEvent {
		t.Error("AC1 FAIL: no stream_event found in output when IncludePartial=true")
	} else {
		t.Log("AC1 PASS: stream_event emitted")
	}

	// AC2: message_start, content_block_delta, and message_stop must appear
	requiredTypes := []string{"message_start", "content_block_delta", "message_stop"}
	for _, et := range requiredTypes {
		if eventTypes[et] > 0 {
			t.Logf("AC2 PASS: %s appeared %d times", et, eventTypes[et])
		} else {
			t.Errorf("AC2 FAIL: %s not found in event types", et)
		}
	}

	// AC7: Every stdout line in stream-json mode parses as valid JSON
	for _, line := range lines {
		if line == "" {
			continue
		}
		var js any
		if err := json.Unmarshal([]byte(line), &js); err != nil {
			t.Errorf("AC7 FAIL: line is not valid JSON: %q - error: %v", line, err)
		}
	}
}

// TestStreamEvent_NotEmittedWhenFlagOff verifies that stream_event is NOT
// emitted when IncludePartial=false.
func TestStreamEvent_NotEmittedWhenFlagOff(t *testing.T) {
	// t.Setenv for iter91 hygiene
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	server := makeMockStreamServerWithPartialEvents(t)
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        true,
			IncludePartial: false, // flag off
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)

	// AC6: With flag off, no stream_event lines appear
	lines := strings.SplitSeq(strings.TrimRight(output, "\n"), "\n")
	for line := range lines {
		if strings.Contains(line, `"type":"stream_event"`) {
			t.Error("AC6 FAIL: stream_event found in output when IncludePartial=false")
			return
		}
	}
	t.Log("AC6 PASS: no stream_event when flag is off")
}

// TestStreamEvent_NotEmittedOnFallback verifies that stream_event is NOT
// emitted when SSE fails and fallback is triggered.
func TestStreamEvent_NotEmittedOnFallback(t *testing.T) {
	// t.Setenv for iter91 hygiene
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	// Server that fails streaming but succeeds on non-streaming
	fallbackCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		// Return502 on streaming endpoint to trigger fallback
		if r.URL.Path == "/v1/messages" && r.URL.Query().Get("stream") == "true" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		// Fallback: non-streaming succeeds
		fallbackCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		jsonResp := `{"id":"msg_fallback","type":"message","role":"assistant","content":[{"type":"text","text":"Fallback response"}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":3}}`
		w.Write([]byte(jsonResp))
	}))
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        true,
			IncludePartial: true, // flag on but fallback should trigger
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)
	t.Logf("Fallback called: %v", fallbackCalled)

	// AC5: Even with flag on, no stream_event on fallback
	lines := strings.SplitSeq(strings.TrimRight(output, "\n"), "\n")
	for line := range lines {
		if strings.Contains(line, `"type":"stream_event"`) {
			t.Error("AC5 FAIL: stream_event found in output on fallback")
			return
		}
	}
	t.Log("AC5 PASS: no stream_event on fallback")
}
