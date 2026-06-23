package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestExtractPromptFromUserMessage(t *testing.T) {
	msg := &SDKUserMessage{
		Type:      "user",
		SessionID: "",
		Message: SDKUserContent{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"text","text":"Hello, Claude!"}]`),
		},
	}

	prompt, err := extractPromptFromUserMessage(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "Hello, Claude!" {
		t.Errorf("expected 'Hello, Claude!', got %q", prompt)
	}
}

func TestExtractPromptFromUserMessageNoText(t *testing.T) {
	msg := &SDKUserMessage{
		Type:      "user",
		SessionID: "",
		Message: SDKUserContent{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"image","source":{}}]`),
		},
	}

	_, err := extractPromptFromUserMessage(msg)
	if err == nil {
		t.Error("expected error for message with no text content")
	}
}

func TestExtractPromptFromUserMessageMultipleBlocks(t *testing.T) {
	msg := &SDKUserMessage{
		Type:      "user",
		SessionID: "",
		Message: SDKUserContent{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"image","source":{}},{"type":"text","text":"Describe this image"}]`),
		},
	}

	prompt, err := extractPromptFromUserMessage(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "Describe this image" {
		t.Errorf("expected 'Describe this image', got %q", prompt)
	}
}

func TestSDKInputReaderReadPrompt(t *testing.T) {
	input := `{"type":"user","session_id":"","message":{"role":"user","content":[{"type":"text","text":"Hello from SDK"}]},"parent_tool_use_id":null}` + "\n"

	reader := NewSDKInputReaderFromReader(strings.NewReader(input))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	prompt, err := reader.ReadPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "Hello from SDK" {
		t.Errorf("expected 'Hello from SDK', got %q", prompt)
	}
}

func TestSDKInputReaderInitialize(t *testing.T) {
	initReq := `{"request_id":"req_123","type":"control_request","request":{"subtype":"initialize","hooks":{"PreToolUse":[{"matcher":"Write|Edit","hookCallbackIds":["hook_0"],"timeout":30000}]}}}` + "\n"
	userMsg := `{"type":"user","session_id":"","message":{"role":"user","content":[{"type":"text","text":"test"}]},"parent_tool_use_id":null}` + "\n"

	pr, pw := io.Pipe()

	reader := NewSDKInputReaderFromReader(pr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	go func() {
		pw.Write([]byte(userMsg))
		pw.Write([]byte(initReq))
	}()

	_, err := reader.WaitForInitialize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.HasHooks("PreToolUse") {
		t.Error("expected PreToolUse hooks to be registered")
	}

	matchers := reader.GetHookMatchers("PreToolUse")
	if len(matchers) != 1 {
		t.Fatalf("expected 1 PreToolUse matcher, got %d", len(matchers))
	}
	if matchers[0].Matcher != "Write|Edit" {
		t.Errorf("expected matcher 'Write|Edit', got %q", matchers[0].Matcher)
	}
	if len(matchers[0].HookCallbackIDs) != 1 || matchers[0].HookCallbackIDs[0] != "hook_0" {
		t.Errorf("expected hookCallbackIds ['hook_0'], got %v", matchers[0].HookCallbackIDs)
	}

	pw.Close()
}

func TestSDKInputReaderInitializeReturnsConfig(t *testing.T) {
	initReq := `{"request_id":"req_cfg","type":"control_request","request":{"subtype":"initialize","hooks":{},"systemPrompt":"custom prompt","appendSystemPrompt":"extra instructions"}}` + "\n"

	pr, pw := io.Pipe()
	reader := NewSDKInputReaderFromReader(pr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	go func() {
		pw.Write([]byte(initReq))
	}()

	initCfg, err := reader.WaitForInitialize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if initCfg.SystemPrompt != "custom prompt" {
		t.Errorf("expected systemPrompt 'custom prompt', got %q", initCfg.SystemPrompt)
	}
	if initCfg.AppendSystemPrompt != "extra instructions" {
		t.Errorf("expected appendSystemPrompt 'extra instructions', got %q", initCfg.AppendSystemPrompt)
	}

	pw.Close()
}

func TestSDKInputReaderInitializeEmptyConfig(t *testing.T) {
	initReq := `{"request_id":"req_empty","type":"control_request","request":{"subtype":"initialize"}}` + "\n"

	pr, pw := io.Pipe()
	reader := NewSDKInputReaderFromReader(pr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	go func() {
		pw.Write([]byte(initReq))
	}()

	initCfg, err := reader.WaitForInitialize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if initCfg.SystemPrompt != "" {
		t.Errorf("expected empty systemPrompt, got %q", initCfg.SystemPrompt)
	}
	if initCfg.AppendSystemPrompt != "" {
		t.Errorf("expected empty appendSystemPrompt, got %q", initCfg.AppendSystemPrompt)
	}

	pw.Close()
}

func TestSDKInputReaderIgnoresKeepAlive(t *testing.T) {
	input := `{"type":"keep_alive"}` + "\n" +
		`{"type":"user","session_id":"","message":{"role":"user","content":[{"type":"text","text":"after keepalive"}]},"parent_tool_use_id":null}` + "\n"

	reader := NewSDKInputReaderFromReader(strings.NewReader(input))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	prompt, err := reader.ReadPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "after keepalive" {
		t.Errorf("expected 'after keepalive', got %q", prompt)
	}
}

func TestSDKInputReaderIgnoresEmptyLines(t *testing.T) {
	input := "\n\n" +
		`{"type":"user","session_id":"","message":{"role":"user","content":[{"type":"text","text":"after empty"}]},"parent_tool_use_id":null}` + "\n"

	reader := NewSDKInputReaderFromReader(strings.NewReader(input))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	prompt, err := reader.ReadPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "after empty" {
		t.Errorf("expected 'after empty', got %q", prompt)
	}
}

func TestSDKInputReaderClosedStdin(t *testing.T) {
	reader := NewSDKInputReaderFromReader(strings.NewReader(""))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	_, err := reader.ReadPrompt(ctx)
	if err == nil {
		t.Error("expected error for closed stdin")
	}
}

func TestSDKInputReaderControlResponse(t *testing.T) {
	pr, pw := io.Pipe()
	reader := NewSDKInputReaderFromReader(pr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader.Start(ctx)

	respChan := make(chan *ControlResponse, 1)
	reader.mu.Lock()
	reader.pendingResponses["test_req_1"] = respChan
	reader.mu.Unlock()

	go func() {
		resp := `{"type":"control_response","response":{"subtype":"success","request_id":"test_req_1","response":{"behavior":"allow"}}}` + "\n"
		pw.Write([]byte(resp))
	}()

	select {
	case resp := <-respChan:
		if resp.Response.Subtype != "success" {
			t.Errorf("expected subtype 'success', got %q", resp.Response.Subtype)
		}
		if resp.Response.RequestID != "test_req_1" {
			t.Errorf("expected request_id 'test_req_1', got %q", resp.Response.RequestID)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for control response")
	}

	pw.Close()
}
