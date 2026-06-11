# API Provider Architecture

The `internal/api/` package provides a unified interface for calling LLM APIs, with pluggable backends ("providers").

## Core Interface

```go
// Provider defines the interface for AI backend providers.
type Provider interface {
    SendMessage(ctx, messages, tools, toolResults, systemPrompt) (*Response, error)
    SendMessageStream(ctx, messages, tools, toolResults, systemPrompt, idleTimeout) (<-chan StreamContentBlock, *StreamResult)
    Kind() ProviderKind
}
```

## Provider Kinds

| Kind | Implementation | Notes |
|------|---------------|-------|
| `anthropic` | `provider_anthropic.go` | Uses `github.com/anthropics/anthropic-sdk-go` |
| `openai` | `provider_openai.go` | Uses `github.com/openai/openai-go/v3` |
| `vertexai` | `provider_vertexai.go` | OpenAI-compatible API, Google Cloud auth |

## Provider Selection

`NewClientWithModel(model)` selects the provider at client creation time based on environment variables:

1. If `OPENAI_BASE_URL` is set → `openAIProvider`
2. If `VERTEXAI_BASE_URL` is set → `vertexAIProvider`
3. Otherwise → `anthropicProvider` (default)

## Adding a New Provider

1. **Create the provider file** in `internal/api/provider_<name>.go`
2. **Implement the `Provider` interface** with `SendMessage`, `SendMessageStream`, and `Kind()`
3. **Add to `NewClientWithModel`** in `client.go` — add an env var check in the selection chain
4. **Add provider kind constant** in `provider.go` if you need a distinct `ProviderKind`
5. **Add tests** — follow the pattern in `provider_openai_test.go`
6. **Update this doc**

### Interface Contract

- `SendMessage` must return a `*Response` with at minimum `Content`, `StopReason`, `Model`, and `Usage` fields populated
- `SendMessageStream` runs in a goroutine, yields `StreamContentBlock` via the channel, and returns a `*StreamResult` when the channel closes
- Both methods call `NormalizeMessages(messages, tools, Capabilities{...})` before building the request
- Both methods must set `StreamResult.StreamComplete = true` only when a terminal stop reason is received
- `StreamResult.Error` should be a plain string (not wrapped error type) for downstream compatibility
- Providers should implement `ProviderWithRetryConfig` to receive shared retry configuration

### Normalization

`NormalizeMessages` is a shared gateway that all providers call. It handles:
- Injecting `__arg__` placeholder for empty tool schemas
- Deduplicating tool results by `ToolUseID`

Providers set `SupportsPromptCaching: true` or `false` in `Capabilities` to control prompt-caching-specific normalization.

### Retry Logic

The `RetryConfig` struct in `retry.go` provides shared retry configuration. Providers should implement `ProviderWithRetryConfig` and call `sendWithRetry` with exponential backoff for HTTP errors. Retryable status codes: 429, 408, 409, 500, 502, 503, 504, 529.

## Shared Types

All types are defined in `client.go`:
- `Message`, `ToolUseBlock`, `ToolResultBlock`, `ToolResult`, `ToolUse`
- `Response`, `ContentBlock`, `Usage`, `ToolParam`, `ToolInputSchema`
- `StreamContentBlock`, `StreamResult`

## Environment Variables

### OpenAI Provider
- `OPENAI_BASE_URL` — API base URL (required)
- `OPENAI_API_KEY` — API key (required)
- `OPENAI_DEFAULT_MODEL` — default model (required when using OpenAI provider)
- `OPENAI_WIRE_API` — wire protocol (`chat` only; `responses` not yet supported)

### Anthropic Provider
- `ANTHROPIC_BASE_URL` — API base URL
- `ANTHROPIC_AUTH_TOKEN` — API key
- `ANTHROPIC_MODEL` — default model
- `ANTHROPIC_BETAS` — comma-separated list of additional beta headers
- `API_TIMEOUT_MS` — request timeout in milliseconds

### Vertex AI Provider
- `VERTEXAI_BASE_URL` — API base URL (required)
- `VERTEXAI_API_KEY` — API key (required)
- `VERTEXAI_DEFAULT_MODEL` — default model (required when using Vertex AI provider)