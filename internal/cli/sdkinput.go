package cli

import (
	"bufio"
	"context"
	stdjson "encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/utils"
)

// SDKInputReader reads NDJSON messages from stdin when --input-format stream-json is active.
// It handles: user messages (prompt extraction), control_request (initialize, interrupt),
// and control_response (replies to jenny-initiated requests like can_use_tool, hook_callback).
type SDKInputReader struct {
	scanner *bufio.Scanner
	stdin   io.Reader

	mu               sync.Mutex
	pendingResponses map[string]chan *ControlResponse
	hooks            map[string][]SDKHookMatcher
	userMessages     chan *SDKUserMessage
	controlRequests  chan *SDKControlRequest
	started          bool
	cancelReadLoop   context.CancelFunc
}

// SDKUserMessage is a user-type message from stdin.
type SDKUserMessage struct {
	Type            string         `json:"type"`
	SessionID       string         `json:"session_id"`
	Message         SDKUserContent `json:"message"`
	ParentToolUseID *string        `json:"parent_tool_use_id"`
}

// SDKUserContent represents the message body of a user message.
type SDKUserContent struct {
	Role    string             `json:"role"`
	Content stdjson.RawMessage `json:"content"`
}

// SDKControlRequest is a control_request message from SDK on stdin.
type SDKControlRequest struct {
	RequestID string             `json:"request_id"`
	Type      string             `json:"type"`
	Request   stdjson.RawMessage `json:"request"`
}

// SDKControlRequestInner is the inner request body.
type SDKControlRequestInner struct {
	Subtype            string                        `json:"subtype"`
	Hooks              map[string]stdjson.RawMessage `json:"hooks,omitempty"`
	SystemPrompt       *string                       `json:"systemPrompt,omitempty"`
	AppendSystemPrompt *string                       `json:"appendSystemPrompt,omitempty"`
	SDKMcpServers      stdjson.RawMessage            `json:"sdkMcpServers,omitempty"`
	JSONSchema         stdjson.RawMessage            `json:"jsonSchema,omitempty"`
	Agents             stdjson.RawMessage            `json:"agents,omitempty"`
}

// SDKHookMatcher describes a registered SDK hook.
type SDKHookMatcher struct {
	Matcher         string   `json:"matcher"`
	HookCallbackIDs []string `json:"hookCallbackIds"`
	Timeout         int      `json:"timeout,omitempty"`
}

// ControlResponse is a control_response message on stdin (SDK replying to jenny's request).
type ControlResponse struct {
	Type     string              `json:"type"`
	Response ControlResponseBody `json:"response"`
}

// ControlResponseBody holds the response body.
type ControlResponseBody struct {
	Subtype   string             `json:"subtype"`
	RequestID string             `json:"request_id"`
	Response  stdjson.RawMessage `json:"response,omitempty"`
	Error     string             `json:"error,omitempty"`
}

// HookCallbackResponse is the parsed response from an SDK hook callback.
type HookCallbackResponse struct {
	Continue           *bool              `json:"continue,omitempty"`
	Decision           string             `json:"decision,omitempty"`
	StopReason         string             `json:"stopReason,omitempty"`
	SuppressOutput     bool               `json:"suppressOutput,omitempty"`
	HookSpecificOutput stdjson.RawMessage `json:"hookSpecificOutput,omitempty"`
}

// HookPreToolUseOutput is the hookSpecificOutput for PreToolUse hooks.
type HookPreToolUseOutput struct {
	HookEventName      string             `json:"hookEventName"`
	PermissionDecision string             `json:"permissionDecision,omitempty"`
	UpdatedInput       stdjson.RawMessage `json:"updatedInput,omitempty"`
}

// NewSDKInputReader creates a new SDK input reader.
func NewSDKInputReader() *SDKInputReader {
	return NewSDKInputReaderFromReader(os.Stdin)
}

// NewSDKInputReaderFromReader creates a new SDK input reader from a custom reader (for testing).
func NewSDKInputReaderFromReader(r io.Reader) *SDKInputReader {
	return &SDKInputReader{
		stdin:            r,
		scanner:          bufio.NewScanner(r),
		pendingResponses: make(map[string]chan *ControlResponse),
		hooks:            make(map[string][]SDKHookMatcher),
		userMessages:     make(chan *SDKUserMessage, 16),
		controlRequests:  make(chan *SDKControlRequest, 16),
	}
}

// Start begins reading stdin in a background goroutine. Call once.
func (r *SDKInputReader) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	readCtx, cancel := context.WithCancel(ctx)
	r.cancelReadLoop = cancel
	r.mu.Unlock()

	go r.readLoop(readCtx)
}

// Stop cancels the read loop.
func (r *SDKInputReader) Stop() {
	r.mu.Lock()
	if r.cancelReadLoop != nil {
		r.cancelReadLoop()
	}
	r.mu.Unlock()
}

func (r *SDKInputReader) readLoop(ctx context.Context) {
	defer close(r.userMessages)
	defer close(r.controlRequests)

	for r.scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := r.scanner.Text()
		if line == "" {
			continue
		}

		var raw map[string]stdjson.RawMessage
		if err := stdjson.Unmarshal([]byte(line), &raw); err != nil {
			log.Warn("sdkinput: invalid JSON line", "error", err)
			continue
		}

		typeField, ok := raw["type"]
		if !ok {
			log.Warn("sdkinput: message missing type field")
			continue
		}

		var msgType string
		if err := stdjson.Unmarshal(typeField, &msgType); err != nil {
			log.Warn("sdkinput: invalid type field", "error", err)
			continue
		}

		switch msgType {
		case "user":
			var msg SDKUserMessage
			if err := stdjson.Unmarshal([]byte(line), &msg); err != nil {
				log.Warn("sdkinput: invalid user message", "error", err)
				continue
			}
			select {
			case r.userMessages <- &msg:
			case <-ctx.Done():
				return
			}

		case "control_request":
			var msg SDKControlRequest
			if err := stdjson.Unmarshal([]byte(line), &msg); err != nil {
				log.Warn("sdkinput: invalid control_request", "error", err)
				continue
			}
			select {
			case r.controlRequests <- &msg:
			case <-ctx.Done():
				return
			}

		case "control_response":
			var resp ControlResponse
			if err := stdjson.Unmarshal([]byte(line), &resp); err != nil {
				log.Warn("sdkinput: invalid control_response", "error", err)
				continue
			}
			r.mu.Lock()
			if ch, ok := r.pendingResponses[resp.Response.RequestID]; ok {
				ch <- &resp
				delete(r.pendingResponses, resp.Response.RequestID)
			}
			r.mu.Unlock()

		case "keep_alive":
			// ignored

		default:
			log.Debug("sdkinput: unknown message type", "type", msgType)
		}
	}
}

// ReadPrompt blocks until the first user message arrives and returns the extracted prompt text.
func (r *SDKInputReader) ReadPrompt(ctx context.Context) (string, error) {
	select {
	case msg, ok := <-r.userMessages:
		if !ok {
			return "", fmt.Errorf("stdin closed before user message received")
		}
		return extractPromptFromUserMessage(msg)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// SDKInitializeConfig holds configuration extracted from the SDK initialize handshake.
type SDKInitializeConfig struct {
	SystemPrompt       string
	AppendSystemPrompt string
}

// WaitForInitialize waits for and handles the initialize control_request.
// Returns the extracted config after sending the control_response on stdout.
func (r *SDKInputReader) WaitForInitialize(ctx context.Context) (*SDKInitializeConfig, error) {
	for {
		select {
		case req, ok := <-r.controlRequests:
			if !ok {
				return nil, fmt.Errorf("stdin closed before initialize received")
			}
			var inner SDKControlRequestInner
			if err := stdjson.Unmarshal(req.Request, &inner); err != nil {
				log.Warn("sdkinput: failed to parse control_request", "error", err)
				continue
			}
			if inner.Subtype == "initialize" {
				r.registerHooks(inner.Hooks)
				cfg := &SDKInitializeConfig{}
				if inner.SystemPrompt != nil {
					cfg.SystemPrompt = *inner.SystemPrompt
				}
				if inner.AppendSystemPrompt != nil {
					cfg.AppendSystemPrompt = *inner.AppendSystemPrompt
				}
				if err := r.sendControlResponse(req.RequestID, "success", stdjson.RawMessage(`{}`)); err != nil {
					return nil, err
				}
				return cfg, nil
			}
			log.Debug("sdkinput: ignoring non-initialize control_request", "subtype", inner.Subtype)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// registerHooks extracts hook definitions from the initialize request.
func (r *SDKInputReader) registerHooks(hooks map[string]stdjson.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for event, raw := range hooks {
		var matchers []SDKHookMatcher
		if err := stdjson.Unmarshal(raw, &matchers); err != nil {
			log.Warn("sdkinput: failed to parse hooks", "event", event, "error", err)
			continue
		}
		r.hooks[event] = matchers
		log.Debug("sdkinput: registered hooks", "event", event, "count", len(matchers))
	}
}

// HasHooks returns true if any hooks are registered for the given event.
func (r *SDKInputReader) HasHooks(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hooks[event]) > 0
}

// GetHookMatchers returns the hook matchers for the given event.
func (r *SDKInputReader) GetHookMatchers(event string) []SDKHookMatcher {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]SDKHookMatcher, len(r.hooks[event]))
	copy(result, r.hooks[event])
	return result
}

// SendHookCallback sends a hook_callback control_request on stdout and waits for the response.
func (r *SDKInputReader) SendHookCallback(ctx context.Context, callbackID string, input map[string]any) (*HookCallbackResponse, error) {
	requestID := utils.NewUUID()

	inputBytes, err := stdjson.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshalling hook input: %w", err)
	}

	request := map[string]any{
		"subtype":     "hook_callback",
		"callback_id": callbackID,
		"input":       stdjson.RawMessage(inputBytes),
	}
	if tuID, ok := input["tool_use_id"]; ok {
		request["tool_use_id"] = tuID
	}

	respChan := make(chan *ControlResponse, 1)
	r.mu.Lock()
	r.pendingResponses[requestID] = respChan
	r.mu.Unlock()

	if err := writeControlRequest(requestID, request); err != nil {
		r.mu.Lock()
		delete(r.pendingResponses, requestID)
		r.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-respChan:
		if resp.Response.Subtype == "error" {
			return nil, fmt.Errorf("hook callback error: %s", resp.Response.Error)
		}
		var hookResp HookCallbackResponse
		if err := stdjson.Unmarshal(resp.Response.Response, &hookResp); err != nil {
			return nil, fmt.Errorf("parsing hook response: %w", err)
		}
		return &hookResp, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pendingResponses, requestID)
		r.mu.Unlock()
		return nil, ctx.Err()
	}
}

// SendCanUseTool sends a can_use_tool control_request on stdout and waits for the response.
func (r *SDKInputReader) SendCanUseTool(ctx context.Context, toolName string, input any, toolUseID string) (map[string]any, error) {
	requestID := utils.NewUUID()

	request := map[string]any{
		"subtype":     "can_use_tool",
		"tool_name":   toolName,
		"input":       input,
		"tool_use_id": toolUseID,
	}

	respChan := make(chan *ControlResponse, 1)
	r.mu.Lock()
	r.pendingResponses[requestID] = respChan
	r.mu.Unlock()

	if err := writeControlRequest(requestID, request); err != nil {
		r.mu.Lock()
		delete(r.pendingResponses, requestID)
		r.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-respChan:
		if resp.Response.Subtype == "error" {
			return nil, fmt.Errorf("can_use_tool error: %s", resp.Response.Error)
		}
		var result map[string]any
		if err := stdjson.Unmarshal(resp.Response.Response, &result); err != nil {
			return nil, fmt.Errorf("parsing can_use_tool response: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pendingResponses, requestID)
		r.mu.Unlock()
		return nil, ctx.Err()
	}
}

// sendControlResponse writes a control_response to stdout.
func (r *SDKInputReader) sendControlResponse(requestID, subtype string, response stdjson.RawMessage) error {
	msg := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    subtype,
			"request_id": requestID,
			"response":   stdjson.RawMessage(response),
		},
	}
	data, err := stdjson.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

// writeControlRequest writes a control_request to stdout.
func writeControlRequest(requestID string, request map[string]any) error {
	msg := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    request,
	}
	data, err := stdjson.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

// extractPromptFromUserMessage extracts the text prompt from an SDKUserMessage.
func extractPromptFromUserMessage(msg *SDKUserMessage) (string, error) {
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := stdjson.Unmarshal(msg.Message.Content, &content); err != nil {
		return "", fmt.Errorf("parsing user message content: %w", err)
	}
	for _, block := range content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in user message")
}
