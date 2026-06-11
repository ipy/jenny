// Package api provides the Vertex AI API client.
package api

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// vertexAIProvider implements the Provider interface using the Vertex AI API.
// Vertex AI uses an OpenAI-compatible API surface, but requires Google Cloud auth.
// This stub is ready for implementation using google-cloud-go or a Vertex AI SDK.
type vertexAIProvider struct {
	model string
}

// ProviderKind for Vertex AI.
const ProviderVertexAI ProviderKind = "vertexai"

// newVertexAIProvider creates a new Vertex AI provider.
func newVertexAIProvider(model string) (*vertexAIProvider, error) {
	baseURL := os.Getenv("VERTEXAI_BASE_URL")
	if baseURL == "" {
		return nil, errors.New("VERTEXAI_BASE_URL is required for Vertex AI provider")
	}

	apiKey := os.Getenv("VERTEXAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("VERTEXAI_API_KEY is required for Vertex AI provider")
	}

	if model == "" {
		model = os.Getenv("VERTEXAI_DEFAULT_MODEL")
	}
	if model == "" {
		return nil, errors.New("VERTEXAI_DEFAULT_MODEL is required when using Vertex AI provider")
	}

	return &vertexAIProvider{
		model: model,
	}, nil
}

// Kind returns the provider kind.
func (p *vertexAIProvider) Kind() ProviderKind {
	return ProviderVertexAI
}

// SetModel sets the model.
func (p *vertexAIProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *vertexAIProvider) GetModel() string {
	return p.model
}

// SendMessage sends a non-streaming message.
// TODO: Implement using openai-go/v3 with Vertex AI base URL and auth.
func (p *vertexAIProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("Vertex AI provider: not yet implemented", "model", p.model)
	return nil, errors.New("Vertex AI provider is not yet implemented")
}

// SendMessageStream sends a streaming message.
// TODO: Implement using openai-go/v3 with Vertex AI base URL and auth.
func (p *vertexAIProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock)
	close(blocksChan)
	log.Debug("Vertex AI provider: not yet implemented", "model", p.model)
	return blocksChan, &StreamResult{
		Error: "Vertex AI provider is not yet implemented",
	}
}