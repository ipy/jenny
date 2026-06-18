package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

type mockRequester struct {
	sendMessageStreamFunc func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult)
	sendMessageFunc       func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error)
}

func (m *mockRequester) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	if m.sendMessageFunc != nil {
		return m.sendMessageFunc(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
	}
	return nil, nil
}

func (m *mockRequester) SendMessageStream(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
	if m.sendMessageStreamFunc != nil {
		return m.sendMessageStreamFunc(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix, idleTimeout, fallbackTimeout, onStreamingFallback)
	}
	ch := make(chan api.StreamContentBlock)
	close(ch)
	return ch, &api.StreamResult{}
}

func (m *mockRequester) SetRetryConfig(cfg api.RetryConfig)       {}
func (m *mockRequester) SetBackground(isBackground bool)          {}
func (m *mockRequester) SetThinkingConfig(cfg api.ThinkingConfig) {}
func (m *mockRequester) SetProviderName(name string)              {}

func TestErrorRecovery_ModelNotFound(t *testing.T) {
	mock := &mockRequester{}

	// First call: streaming ModelNotFound
	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error:         "model not found",
			ErrorCategory: api.CategoryModelNotFound,
		}
	}

	// Second call: non-streaming SendMessage succeeds
	mock.sendMessageFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
		return &api.Response{
			Content: []api.ContentBlock{{Type: api.BlockTypeText, Text: "Fallback success"}},
			Model:   "fallback-model",
			Usage:   api.Usage{InputTokens: 10, OutputTokens: 10},
		}, nil
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	result, err := engine.SubmitMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if result != "Fallback success" {
		t.Errorf("Expected 'Fallback success', got '%s'", result)
	}
	if engine.model != "fallback-model" {
		t.Errorf("Expected model to be updated to 'fallback-model', got '%s'", engine.model)
	}
}

func TestErrorRecovery_ModelNotFound_Exhausted(t *testing.T) {
	mock := &mockRequester{}

	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error:         "model not found",
			ErrorCategory: api.CategoryModelNotFound,
		}
	}

	mock.sendMessageFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
		return nil, &api.HTTPError{
			ErrorCategory: api.CategoryModelNotFound,
			Message:       "all models failed",
		}
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	_, err := engine.SubmitMessage(context.Background(), "hello")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	var mnfe *ModelNotFoundError
	if !errors.As(err, &mnfe) {
		t.Fatalf("Expected ModelNotFoundError, got %T: %v", err, err)
	}
	if len(mnfe.Attempted) != 1 || mnfe.Attempted[0] != "initial-model" {
		t.Errorf("Unexpected attempted models: %v", mnfe.Attempted)
	}
}

func TestErrorRecovery_QuotaExhausted(t *testing.T) {
	mock := &mockRequester{}

	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error:         "quota exceeded",
			ErrorCategory: api.CategoryQuotaExhausted,
		}
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	_, err := engine.SubmitMessage(context.Background(), "hello")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Quota exceeded") {
		t.Errorf("Expected quota exceeded message, got: %v", err)
	}
}

func TestErrorRecovery_ContentFilter(t *testing.T) {
	mock := &mockRequester{}

	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error:         "content blocked",
			ErrorCategory: api.CategoryContentFilter,
		}
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	_, err := engine.SubmitMessage(context.Background(), "hello")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Content blocked") {
		t.Errorf("Expected content blocked message, got: %v", err)
	}
}

func TestErrorRecovery_ErrorInfo_Priority(t *testing.T) {
	mock := &mockRequester{}

	// Test that ErrorInfo.Category overrides StreamResult.ErrorCategory
	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error:         "raw error",
			ErrorCategory: api.CategoryUnknown,
			ErrorInfo: &api.ErrorInfo{
				Category: api.CategoryContentFilter,
				Message:  "moderated content",
			},
		}
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	_, err := engine.SubmitMessage(context.Background(), "hello")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Content blocked") {
		t.Errorf("Expected content blocked message (from path triggered by ErrorInfo.Category), got: %v", err)
	}
}

func TestErrorRecovery_PaymentRequired(t *testing.T) {
	mock := &mockRequester{}

	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error:         "payment required",
			ErrorCategory: api.CategoryPaymentRequired,
		}
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	_, err := engine.SubmitMessage(context.Background(), "hello")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Quota exceeded") {
		t.Errorf("Expected quota exceeded message for payment required, got: %v", err)
	}
}

func TestErrorRecovery_ModelNotFound_ErrorInfo(t *testing.T) {
	mock := &mockRequester{}

	// First call: streaming ModelNotFound via ErrorInfo
	mock.sendMessageStreamFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
		ch := make(chan api.StreamContentBlock)
		close(ch)
		return ch, &api.StreamResult{
			Error: "model not found",
			ErrorInfo: &api.ErrorInfo{
				Category: api.CategoryModelNotFound,
			},
		}
	}

	// Second call: non-streaming SendMessage succeeds
	mock.sendMessageFunc = func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
		return &api.Response{
			Content: []api.ContentBlock{{Type: api.BlockTypeText, Text: "Fallback success"}},
			Model:   "fallback-model",
			Usage:   api.Usage{InputTokens: 10, OutputTokens: 10},
		}, nil
	}

	cfg := &StreamConfig{Enabled: false}
	engine, _ := NewQueryEngine(cfg, nil, "initial-model", WithClient(mock))

	result, err := engine.SubmitMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if result != "Fallback success" {
		t.Errorf("Expected 'Fallback success', got '%s'", result)
	}
}
