package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// mockSideQueryClient captures the messages sent to the API.
type mockSideQueryClient struct {
	lastMessages     []api.Message
	lastSystemPrompt []string
	lastTools        []api.ToolParam
	response         *api.Response
	err              error
}

func (m *mockSideQueryClient) SendMessage(_ context.Context, messages []api.Message, tools []api.ToolParam, _ []api.ToolResult, systemPrompt []string, _ string) (*api.Response, error) {
	m.lastMessages = messages
	m.lastSystemPrompt = systemPrompt
	m.lastTools = tools
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &api.Response{
		Content:    []api.ContentBlock{{Type: api.BlockTypeText, Text: "summary result"}},
		StopReason: "end_turn",
		Model:      "test-model",
		Usage:      api.Usage{InputTokens: 100, OutputTokens: 50},
	}, nil
}

func (m *mockSideQueryClient) SendMessageStream(context.Context, []api.Message, []api.ToolParam, []api.ToolResult, []string, string, time.Duration, time.Duration, func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
	ch := make(chan api.StreamContentBlock)
	close(ch)
	return ch, &api.StreamResult{}
}

func (m *mockSideQueryClient) SetRetryConfig(api.RetryConfig)       {}
func (m *mockSideQueryClient) SetBackground(bool)                   {}
func (m *mockSideQueryClient) SetThinkingConfig(api.ThinkingConfig) {}
func (m *mockSideQueryClient) SetProviderName(string)               {}
func (m *mockSideQueryClient) SetMaxTokensOverride(int)             {}
func (m *mockSideQueryClient) SetDynamicThinkingBudget(int)         {}

func newTestSideQueryContext(mock api.Requester) *SideQueryContext {
	return &SideQueryContext{
		Model:     "test-model",
		CostState: &CostState{ModelUsage: make(map[string]ModelUsage)},
		CostMu:    &sync.Mutex{},
		ClientFactory: func(model string) (api.Requester, error) {
			return mock, nil
		},
	}
}

func TestRenderConversationText_BasicExchange(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Hello, can you help me?"},
		{Role: "assistant", Content: "Sure, what do you need?"},
		{Role: "user", Content: "Fix the bug in main.go"},
	}

	text := renderConversationText(messages)

	if !strings.Contains(text, "[user]") {
		t.Error("expected [user] label in rendered text")
	}
	if !strings.Contains(text, "[assistant]") {
		t.Error("expected [assistant] label in rendered text")
	}
	if !strings.Contains(text, "Hello, can you help me?") {
		t.Error("expected user message content in rendered text")
	}
	if !strings.Contains(text, "Fix the bug in main.go") {
		t.Error("expected second user message content in rendered text")
	}
}

func TestRenderConversationText_ToolUseAndResults(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Read the file"},
		{
			Role:    "assistant",
			Content: "Let me read that.",
			ToolUse: []api.ToolUseBlock{
				{ID: "tu_1", Name: "read", Input: map[string]any{"path": "/tmp/test.go"}},
			},
		},
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tu_1", Content: "package main\nfunc main() {}"},
			},
		},
	}

	text := renderConversationText(messages)

	if !strings.Contains(text, "[tool_use: read") {
		t.Error("expected [tool_use: read ...] label")
	}
	if !strings.Contains(text, `"/tmp/test.go"`) {
		t.Error("expected tool input path in rendered text")
	}
	if !strings.Contains(text, "package main") {
		t.Error("expected tool result content")
	}
	if !strings.Contains(text, "[tool_result]") {
		t.Error("expected [tool_result] label")
	}
}

func TestRenderConversationText_StripsThinkingAndSignature(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{
			Role:      "assistant",
			Content:   "visible response",
			Thinking:  "secret thinking block",
			Signature: "sig-xyz",
		},
	}

	text := renderConversationText(messages)

	if strings.Contains(text, "secret thinking") {
		t.Error("thinking content should not appear in rendered text")
	}
	if strings.Contains(text, "sig-xyz") {
		t.Error("signature should not appear in rendered text")
	}
	if !strings.Contains(text, "visible response") {
		t.Error("visible content should still appear")
	}
}

func TestRenderConversationText_TruncatesLargeToolResults(t *testing.T) {
	largeResult := strings.Repeat("x", 3000)
	messages := []api.Message{
		{Role: "user", Content: "run command"},
		{
			Role: "assistant",
			ToolUse: []api.ToolUseBlock{
				{ID: "tu_1", Name: "bash", Input: map[string]any{"command": "ls"}},
			},
		},
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tu_1", Content: largeResult},
			},
		},
	}

	text := renderConversationText(messages)

	if strings.Contains(text, largeResult) {
		t.Error("full large tool result should have been truncated")
	}
	if !strings.Contains(text, "[truncated]") {
		t.Error("expected truncation marker in rendered text")
	}
}

func TestRenderConversationText_ErrorToolResult(t *testing.T) {
	messages := []api.Message{
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tu_1", Content: "command not found", IsError: true},
			},
		},
	}

	text := renderConversationText(messages)

	if !strings.Contains(text, "[tool_result: error]") {
		t.Error("expected [tool_result: error] label for error results")
	}
}

func TestRenderConversationText_MediaStripped(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Look at this ![screenshot](http://example.com/img.png) and fix it"},
	}

	text := renderConversationText(messages)

	if strings.Contains(text, "http://example.com/img.png") {
		t.Error("image URL should be stripped from user content")
	}
	if !strings.Contains(text, "[image]") {
		t.Error("image URL should be replaced with [image] marker")
	}
}

func TestRenderConversationText_Empty(t *testing.T) {
	text := renderConversationText(nil)
	if text != "" {
		t.Errorf("expected empty string for nil messages, got %q", text)
	}

	text = renderConversationText([]api.Message{})
	if text != "" {
		t.Errorf("expected empty string for empty messages, got %q", text)
	}
}

func TestRenderConversationText_MultipleToolUses(t *testing.T) {
	messages := []api.Message{
		{
			Role: "assistant",
			ToolUse: []api.ToolUseBlock{
				{ID: "tu_1", Name: "read", Input: map[string]any{"path": "a.go"}},
				{ID: "tu_2", Name: "glob", Input: map[string]any{"pattern": "*.go"}},
			},
		},
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tu_1", Content: "file a content"},
				{ToolUseID: "tu_2", Content: "a.go\nb.go"},
			},
		},
	}

	text := renderConversationText(messages)

	if !strings.Contains(text, "[tool_use: read") {
		t.Error("expected [tool_use: read ...] label")
	}
	if !strings.Contains(text, `"a.go"`) {
		t.Error("expected tool input path for read tool")
	}
	if !strings.Contains(text, "[tool_use: glob") {
		t.Error("expected [tool_use: glob ...] label")
	}
	if !strings.Contains(text, `"*.go"`) {
		t.Error("expected glob pattern in tool input")
	}
	if strings.Count(text, "[tool_result]") != 2 {
		t.Errorf("expected 2 [tool_result] labels, got %d", strings.Count(text, "[tool_result]"))
	}
}

func TestRenderConversationText_LargeToolInputTruncated(t *testing.T) {
	largeContent := strings.Repeat("x", 1000)
	messages := []api.Message{
		{
			Role: "assistant",
			ToolUse: []api.ToolUseBlock{
				{ID: "tu_1", Name: "write", Input: map[string]any{
					"path":    "/tmp/big.txt",
					"content": largeContent,
				}},
			},
		},
	}

	text := renderConversationText(messages)

	if strings.Contains(text, largeContent) {
		t.Error("full large tool input should have been truncated")
	}
	if !strings.Contains(text, "[tool_use: write") {
		t.Error("expected [tool_use: write ...] label")
	}
	if !strings.Contains(text, "...truncated") {
		t.Error("expected truncation marker in tool input")
	}
}

func TestRenderConversationText_EmptyInput(t *testing.T) {
	messages := []api.Message{
		{
			Role: "assistant",
			ToolUse: []api.ToolUseBlock{
				{ID: "tu_1", Name: "bash", Input: map[string]any{}},
			},
		},
	}

	text := renderConversationText(messages)

	if !strings.Contains(text, "[tool_use: bash]") {
		t.Error("expected [tool_use: bash] without input for empty map")
	}
}

func TestExecuteSummary_SendsSingleUserMessage(t *testing.T) {
	mock := &mockSideQueryClient{}
	sqctx := newTestSideQueryContext(mock)

	sq := &SideQuery{
		Name: "test_summary",
		Messages: []api.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
			{Role: "user", Content: "Do the thing"},
		},
		Prompt:              "Summarize this conversation.",
		MaxTokens:           1000,
		SummarySystemPrompt: []string{"You are a summarizer."},
		DisableThinking:     true,
	}

	result, err := sq.ExecuteSummary(context.Background(), sqctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.lastMessages) != 1 {
		t.Fatalf("expected exactly 1 message sent to API, got %d", len(mock.lastMessages))
	}

	msg := mock.lastMessages[0]
	if msg.Role != "user" {
		t.Errorf("expected single user message, got role=%q", msg.Role)
	}
	if !strings.Contains(msg.Content, "[user]") {
		t.Error("expected rendered conversation with [user] labels in single message")
	}
	if !strings.Contains(msg.Content, "Hello") {
		t.Error("expected original user content in rendered message")
	}
	if !strings.Contains(msg.Content, "Summarize this conversation.") {
		t.Error("expected prompt appended to the single message")
	}

	if mock.lastTools != nil {
		t.Error("expected nil tools for summary mode")
	}

	if result.Mode != SideQueryModeSummary {
		t.Errorf("expected summary mode in result, got %v", result.Mode)
	}
}

func TestExecuteSummary_NoPrompt(t *testing.T) {
	mock := &mockSideQueryClient{}
	sqctx := newTestSideQueryContext(mock)

	sq := &SideQuery{
		Name: "test_summary_no_prompt",
		Messages: []api.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		MaxTokens:           1000,
		SummarySystemPrompt: []string{"You are a summarizer."},
		DisableThinking:     true,
	}

	_, err := sq.ExecuteSummary(context.Background(), sqctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.lastMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mock.lastMessages))
	}

	msg := mock.lastMessages[0]
	if !strings.Contains(msg.Content, "[user]") {
		t.Error("expected conversation transcript in message")
	}
}

func TestExecuteSummary_RespectsMaxMessages(t *testing.T) {
	mock := &mockSideQueryClient{}
	sqctx := newTestSideQueryContext(mock)

	messages := []api.Message{
		{Role: "user", Content: "message 1"},
		{Role: "assistant", Content: "response 1"},
		{Role: "user", Content: "message 2"},
		{Role: "assistant", Content: "response 2"},
		{Role: "user", Content: "message 3"},
		{Role: "assistant", Content: "response 3"},
	}

	sq := &SideQuery{
		Name:                "test_max_messages",
		Messages:            messages,
		MaxTokens:           1000,
		SummarySystemPrompt: []string{"Summarize."},
		SummaryMaxMessages:  2,
		DisableThinking:     true,
	}

	_, err := sq.ExecuteSummary(context.Background(), sqctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := mock.lastMessages[0]
	if strings.Contains(msg.Content, "message 1") {
		t.Error("message 1 should have been truncated by SummaryMaxMessages")
	}
	if !strings.Contains(msg.Content, "response 3") {
		t.Error("last message should be included")
	}
}

func TestExecuteSummary_SystemPromptPassedThrough(t *testing.T) {
	mock := &mockSideQueryClient{}
	sqctx := newTestSideQueryContext(mock)

	sq := &SideQuery{
		Name:                "test_system_prompt",
		Messages:            []api.Message{{Role: "user", Content: "Hello"}},
		MaxTokens:           1000,
		SummarySystemPrompt: []string{"Custom system prompt for summary."},
		DisableThinking:     true,
	}

	_, err := sq.ExecuteSummary(context.Background(), sqctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.lastSystemPrompt) != 1 || mock.lastSystemPrompt[0] != "Custom system prompt for summary." {
		t.Errorf("expected custom system prompt, got %v", mock.lastSystemPrompt)
	}
}
