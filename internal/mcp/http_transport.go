package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/log"
)

func httpRequestTimeout() time.Duration {
	if v := os.Getenv("MCP_HTTP_REQUEST_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 120 * time.Second
}

// HTTPTransport implements the MCP Streamable HTTP transport (spec 2025-03-26).
// All JSON-RPC messages are sent via HTTP POST. The server may respond with
// either application/json or text/event-stream (SSE).
type HTTPTransport struct {
	url          string
	headers      map[string]string
	sessionID    string
	client       *http.Client
	closed       bool
	notifHandler func(Notification)
	mu           sync.Mutex
}

// NewHTTPTransport creates a new HTTP transport for the given MCP endpoint URL.
// Custom headers (e.g., Authorization) are included in every request.
// Timeout is configurable via MCP_HTTP_REQUEST_TIMEOUT env var (seconds).
func NewHTTPTransport(url string, headers map[string]string) *HTTPTransport {
	return &HTTPTransport{
		url:     url,
		headers: headers,
		client: &http.Client{
			Timeout: httpRequestTimeout(),
		},
	}
}

// SetNotificationHandler registers a callback for incoming notifications.
func (t *HTTPTransport) SetNotificationHandler(handler func(Notification)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.notifHandler = handler
}

// BackgroundListen established a long-lived SSE connection for notifications.
// Per MCP spec, the client should maintain one SSE connection for the session.
func (t *HTTPTransport) BackgroundListen(ctx context.Context) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	url := t.url
	t.mu.Unlock()

	// Establish SSE connection
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("SSE connection failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("SSE connection rejected: HTTP %d", resp.StatusCode)
	}

	// Capture session ID if provided
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var currentEvent string
		var dataLines []string

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if len(dataLines) > 0 {
					data := strings.Join(dataLines, "\n")
					if currentEvent == "" || currentEvent == "message" {
						var msg struct {
							Method string          `json:"method"`
							Params json.RawMessage `json:"params"`
						}
						if err := json.Unmarshal([]byte(data), &msg); err == nil && msg.Method != "" {
							t.mu.Lock()
							handler := t.notifHandler
							t.mu.Unlock()
							if handler != nil {
								handler(Notification{
									Method: msg.Method,
									Params: msg.Params,
								})
							}
						}
					}
					dataLines = nil
					currentEvent = ""
				}
				continue
			}

			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
	}()

	return nil
}

// SendRequest sends a JSON-RPC request via HTTP POST and returns the response.
// Handles both application/json and text/event-stream responses per MCP spec.
func (t *HTTPTransport) SendRequest(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport is closed")
	}
	sessionID := t.sessionID
	t.mu.Unlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			t.mu.Lock()
			t.sessionID = sid
			t.mu.Unlock()
		}
		return t.parseResponse(resp, req.ID)

	case http.StatusAccepted:
		// 202 is only valid for notifications; if a request receives it,
		// the server is misbehaving.
		if req.ID != nil {
			return nil, fmt.Errorf("unexpected HTTP 202 for JSON-RPC request (id=%v)", req.ID)
		}
		return nil, nil

	case http.StatusNotFound:
		t.mu.Lock()
		t.sessionID = ""
		t.mu.Unlock()
		return nil, &SessionExpiredError{SessionID: sessionID}

	default:
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

// SendNotification sends a JSON-RPC notification via HTTP POST.
// Expects HTTP 202 Accepted with no response body.
func (t *HTTPTransport) SendNotification(ctx context.Context, notif jsonRPCRequest) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	sessionID := t.sessionID
	t.mu.Unlock()

	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling notification: %w", err)
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, httpRequestTimeout())
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP notification failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification rejected: HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// Close shuts down the HTTP transport. If a session is active, sends DELETE.
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	if t.sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil)
		if err == nil {
			req.Header.Set("Mcp-Session-Id", t.sessionID)
			for k, v := range t.headers {
				req.Header.Set(k, v)
			}
			resp, err := t.client.Do(req)
			if err != nil {
				log.Debug("MCP session DELETE failed", "error", err)
			} else {
				resp.Body.Close()
				if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
					log.Debug("MCP session DELETE rejected", "status", resp.StatusCode)
				}
			}
		}
		t.sessionID = ""
	}

	return nil
}

// parseResponse parses the HTTP response based on Content-Type.
// requestID is passed to the SSE parser for response matching.
func (t *HTTPTransport) parseResponse(resp *http.Response, requestID any) (*jsonRPCResponse, error) {
	contentType := resp.Header.Get("Content-Type")

	switch {
	case strings.HasPrefix(contentType, "application/json"):
		return t.parseJSONResponse(resp.Body)
	case strings.HasPrefix(contentType, "text/event-stream"):
		return t.parseSSEResponse(resp.Body, requestID)
	default:
		return t.parseJSONResponse(resp.Body)
	}
}

// parseJSONResponse parses a single JSON-RPC response from the body.
func (t *HTTPTransport) parseJSONResponse(body io.Reader) (*jsonRPCResponse, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing JSON response: %w", err)
	}

	return &resp, nil
}

// parseSSEResponse reads an SSE stream and extracts the JSON-RPC response
// matching the given requestID. Per MCP spec, the response is delivered as an
// SSE event with event type "message". Server may send notifications before
// the final response; we match by ID to ensure correctness.
func (t *HTTPTransport) parseSSEResponse(body io.Reader, requestID any) (*jsonRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	var currentEvent string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				if currentEvent == "" || currentEvent == "message" {
					var msg struct {
						ID     any             `json:"id"`
						Method string          `json:"method"`
						Params json.RawMessage `json:"params"`
						Result json.RawMessage `json:"result"`
						Error  *jsonRPCError   `json:"error"`
					}
					if err := json.Unmarshal([]byte(data), &msg); err == nil {
						if msg.ID != nil {
							if matchesRequestID(msg.ID, requestID) {
								return &jsonRPCResponse{
									JSONRPC: "2.0",
									ID:      msg.ID,
									Result:  msg.Result,
									Error:   msg.Error,
								}, nil
							}
						} else if msg.Method != "" {
							// It's a notification
							t.mu.Lock()
							handler := t.notifHandler
							t.mu.Unlock()
							if handler != nil {
								handler(Notification{
									Method: msg.Method,
									Params: msg.Params,
								})
							}
						}
					}
				}
				dataLines = nil
				currentEvent = ""
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// Process any remaining buffered event
	if len(dataLines) > 0 {
		data := strings.Join(dataLines, "\n")
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err == nil {
			if matchesRequestID(resp.ID, requestID) {
				return &resp, nil
			}
		}
	}

	return nil, fmt.Errorf("SSE stream ended without receiving response for request ID %v", requestID)
}

// matchesRequestID checks if a response ID matches the request ID.
// JSON numbers may deserialize as float64, so we normalize for comparison.
func matchesRequestID(respID, reqID any) bool {
	if respID == nil || reqID == nil {
		return false
	}
	return fmt.Sprintf("%v", respID) == fmt.Sprintf("%v", reqID)
}
