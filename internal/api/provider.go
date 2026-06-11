// Package api provides the Anthropic API client.
package api

import (
	"context"
	"time"
)

// ProviderKind represents the type of provider backend.
type ProviderKind string

const (
	ProviderAnthropic ProviderKind = "anthropic"
	ProviderOpenAI    ProviderKind = "openai"
)

// Provider defines the interface for AI backend providers.
// Each provider implements the SendMessage and SendMessageStream methods
// for communicating with a specific AI API backend.
type Provider interface {
	// SendMessage sends a non-streaming message and returns the response.
	// systemPrompt is the cached stable prefix; systemPromptSuffix is the per-turn dynamic part (no cache control).
	SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error)

	// SendMessageStream sends a streaming message and yields content blocks via the channel.
	// systemPrompt is the cached stable prefix; systemPromptSuffix is the per-turn dynamic part (no cache control).
	SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult)

	// Kind returns the provider kind for debugging/logging.
	Kind() ProviderKind
}

// ProviderWithRetryConfig allows providers to receive retry configuration.
type ProviderWithRetryConfig interface {
	Provider
	SetRetryConfig(cfg RetryConfig)
}
