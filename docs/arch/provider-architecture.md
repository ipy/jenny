---
title: API Provider Architecture
slug: provider-architecture
priority: P0
status: partial
spec: partial
code: done
package: internal/api
gaps:
  - "ThinkingConfig / ProviderWithThinkingConfig interface undocumented"
  - "Requester interface and Client delegation layer undocumented"
  - "Streaming fallback mechanism undocumented"
  - "Vertex AI backend path not implemented despite documentation"
depends_on:
  - anthropic-api-client
  - openai-api-client
---
# API Provider Architecture

The `internal/api/` package provides a unified interface for calling LLM APIs, with pluggable backends ("providers").

## Core Interface

```go
// Provider defines the interface for AI backend providers.
type Provider interface {
    SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix) (*Response, error)
    SendMessageStream(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix, idleTimeout) (<-chan StreamContentBlock, *StreamResult)
    Kind() ProviderKind
    SetProviderName(name string)
    SupportsNativeSearch() bool
}
```

## Provider Kinds

| Kind | Description |
|------|-------------|
| `anthropic` | Anthropic API (Claude models) |
| `openai` | OpenAI Chat Completions API (`/v1/chat/completions`) |
| `openai_responses` | OpenAI Responses API (`/v1/responses`) — selected via `OPENAI_WIRE_API=responses` |
| `genai` | Google GenAI (Gemini models) |

## Provider Selection

`NewClientWithModel(model)` selects the provider at client creation time based on environment variables:

1. If `OPENAI_BASE_URL` is set → `openAIProvider` (or `openAIResponsesProvider` if `OPENAI_WIRE_API=responses`)
2. If `GENAI_BASE_URL` is set → `genaiProvider`
3. If `GENAI_API_KEY` is set → `genaiProvider`
4. If `GOOGLE_API_KEY` or `GEMINI_API_KEY` is set → `genaiProvider`
5. If `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` are set → `genaiProvider`
6. If `GOOGLE_GENAI_USE_VERTEXAI=1|true` → `genaiProvider`
7. Otherwise → `anthropicProvider` (default)

`DetectAPIKeySource()` returns the detected provider name ("openai", "genai", "anthropic", "none") and is consumed by the agent loop for cost tracking.

## Core Implementation Strategy: Surgical HTTP Clients

In June 2026, the architecture underwent a major optimization to address "SDK Bloat." The official SDKs (`openai-go`, `anthropic-sdk-go`, `google.golang.org/genai`) were found to contribute nearly 40MB of binary bloat due to extensive code generation for unsupported endpoints (e.g., Audio, Fine-tuning) and heavy use of generics.

Jenny now implements a **Surgical HTTP Client** approach:
1. **Lightweight Type Definitions:** Only the necessary struct types for the endpoints used (e.g., `/chat/completions`, `/v1/messages`) are defined, in per-provider type files.
2. **Polymorphic Field Handling:** `json.RawMessage` and custom marshaling logic handle complex, multi-modal API fields (like Anthropic's `content` arrays or Gemini's `parts`) without needing thousands of generated helper methods.
3. **Common Transport:** A shared HTTP client handles retries, context cancellation, and Server-Sent Events (SSE) parsing.
4. **Binary Size Impact:** This approach reduced the stripped binary size from ~34MB to **<8MB**, significantly improving startup time and distribution size.

## Adding a New Provider

1. **Create a provider implementation** implementing the `Provider` interface with `SendMessage`, `SendMessageStream`, and `Kind()`
2. **Register the provider** in the client selection chain with an environment variable check
3. **Add a provider kind constant** if a distinct `ProviderKind` is needed
4. **Add tests** — follow the pattern of existing provider tests
5. **Update this doc**

### Interface Contract

- `SendMessage` must return a `*Response` with at minimum `Content`, `StopReason`, `Model`, and `Usage` fields populated
- `SendMessageStream` runs in a goroutine, yields `StreamContentBlock` via the channel, and returns a `*StreamResult` when the channel closes
- Both methods call `NormalizeMessages(messages, tools, Capabilities{...})` before building the request
- Both methods must set `StreamResult.StreamComplete = true` when a finish/stop reason is received (note: Anthropic uses `hasMessageStop`, OpenAI/GenAI use any non-empty `finish_reason` including `max_tokens`)
- `StreamResult.Error` should be a plain string (not wrapped error type) for downstream compatibility
- Providers should implement `ProviderWithRetryConfig` to receive shared retry configuration
- `SupportsNativeSearch()` must return `true` for providers with native web search capability (Anthropic, OpenAI, OpenAI Responses, GenAI); other providers return `false`
- `SetProviderName(name)` sets a human-readable provider name for logging and display

### Stream Event Normalization

All providers MUST emit `StreamContentBlock` entries with `Type: "stream_event"` carrying an `AnthropicStreamEvent` in `RawEvent`. This means non-Anthropic providers (OpenAI, GenAI) translate their native streaming events into Anthropic-compatible `AnthropicStreamEvent` structs. This ensures the engine loop and `stream-json` output produce identical `stream_event` shapes regardless of backend — matching the Claude Code reference format.

The Anthropic event types emitted are: `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`.

### System Prompt Multi-Block Handling

The system prompt consists of multiple logical blocks ordered by stability (see [`system-prompt.md`](./system-prompt.md)), followed by an optional `systemPromptSuffix` for dynamic per-turn content. All providers MUST preserve these as separate blocks in the request payload to maximize caching efficiency:

| Provider | Mechanism |
|----------|-----------|
| Anthropic | Multiple `AnthropicContentBlock` in the `system` array; `cache_control` on the last stable block. |
| OpenAI ChatCompletion | Single `role: system` message with multiple text content blocks at the start of the `messages` array. |
| OpenAI Responses | First system prompt block goes to top-level `instructions` field; subsequent blocks + suffix go as a `role: system` message in the `input` array. |
| GenAI | Single `SystemInstruction` block with multiple `GenAIPart` entries. |

Concatenating the suffix into the system prompt would invalidate the cache on every turn.

### Normalization

`NormalizeMessages` is a shared gateway that all providers call. It handles:
- Injecting `__arg__` placeholder for empty tool schemas
- Deduplicating tool results by `ToolUseID`

Providers set `SupportsPromptCaching: true` or `false` in `Capabilities` to control prompt-caching-specific normalization.

### Retry Logic

Shared retry configuration is available for all providers. Providers should implement the retry config interface and use the shared retry mechanism with exponential backoff for HTTP errors. Retryable status codes: 429, 408, 409, 500, 502, 503, 504, 529.

## Shared Types and Constants

Core message and streaming types are defined centrally:
- Message, tool use/result blocks, response, content block, usage, tool parameters
- Streaming content block and stream result types

Content block, event, delta, and role constants are centralized in a shared constants file, ensuring consistent string values across all providers.

Providers MUST use these constants rather than string literals for content block types and roles.

## Environment Variables

### OpenAI Provider
- `OPENAI_BASE_URL` — API base URL (required)
- `OPENAI_API_KEY` — API key (required)
- `OPENAI_DEFAULT_MODEL` — default model (required when using OpenAI provider)
- `OPENAI_WIRE_API` — wire protocol: `chat` (default, ChatCompletion API) or `responses` (Responses API). Both support full streaming.

### Anthropic Provider
- `ANTHROPIC_BASE_URL` — API base URL
- `ANTHROPIC_AUTH_TOKEN` — API key (note: `DetectAPIKeySource` and normalization also check `ANTHROPIC_API_KEY`)
- `ANTHROPIC_MODEL` — default model
- `ANTHROPIC_BETAS` — comma-separated list of additional beta headers
- `API_TIMEOUT_MS` — request timeout in milliseconds

### GenAI Provider (Gemini)
The `genaiProvider` uses a surgical HTTP client (like all other providers) to call the Gemini REST API at `generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`. It does **not** use Google's `google.golang.org/genai` Go SDK. All GenAI types are lightweight local structs in `genai_types.go`.

Environment variables:
- `GENAI_BASE_URL` — override the API base URL (e.g. proxy or VPC endpoint). Optional.
- `GENAI_API_KEY` — explicit API key. Highest precedence.
- `GOOGLE_API_KEY` / `GEMINI_API_KEY` — Gemini API key.
- `GOOGLE_CLOUD_PROJECT` — triggers GenAI provider selection (but Vertex AI backend is not yet implemented).
- `GOOGLE_CLOUD_LOCATION` (or `GOOGLE_CLOUD_REGION`) — triggers GenAI provider selection.
- `GOOGLE_GENAI_USE_VERTEXAI=1|true` — triggers GenAI provider selection.
- `GENAI_DEFAULT_MODEL` — default model (required when using the genai provider).

**Note:** The Vertex AI backend path is not yet implemented. The provider always defaults to `https://generativelanguage.googleapis.com` regardless of `GOOGLE_CLOUD_PROJECT`/`GOOGLE_CLOUD_LOCATION` settings.

Behavior:
- Non-streaming and streaming requests use direct REST calls via `HTTPClient.Request` and `HTTPClient.StreamRequest`.
- System prompt and suffix are sent as separate `GenAIPart` entries within the single `SystemInstruction` content block.
- Tools are translated from `ToolParam` to GenAI `FunctionDeclaration` entries. Empty property sets receive a synthetic `__arg__: string` placeholder.
- Tool results are sent as standalone `functionResponsePart` entries on a `user` content turn.
- Errors use the standard `classifyErrorDomestic`/`classifyErrorInternational`/`classifyErrorCommon` pipeline.
- Usage tokens: `PromptTokenCount → InputTokens`, `ResponseTokenCount → OutputTokens`, `CachedContentTokenCount → CacheReadInputTokens`, `ThoughtsTokenCount` is folded into `OutputTokens`.

## Max Tokens Resolution

Every API request carries a `max_tokens` value that is guaranteed not to exceed the active model's actual output capability. Resolution is centralized in a single function, `ResolveMaxTokens(model, override)`, which consults a bundled per-model capability table.

### Invariant

All three request sites (streaming, non-streaming, and the streaming-fallback path) call `ResolveMaxTokens` at request-build time. No code path hard-codes a `max_tokens` value or consults a provider-local default.

### Resolution Rules

1. If `override > 0` and within the model's capability, return `override` unchanged.
2. If `override > 0` and exceeds the model's capability, return the capability value and emit a WARN log with reason `override_exceeds_capability`.
3. If `override <= 0`, return the model's full capability as the default (a negative override is treated as 0 and logged with reason `negative_override`).
4. For unknown models, return the conservative fallback of 16384 and emit a WARN log with reason `unknown_model_capability_default`.

### Provider-Prefix Normalization

Model identifiers from multi-provider endpoints (e.g. OpenRouter, Cloudflare Workers AI) may carry provider prefixes such as `workers-ai/@cf/meta/llama-3.1-8b-instruct-fp8`, `deepseek/deepseek-v4-pro`, or `deepseek/deepseek-v4-pro:thinking`. Resolution uses a two-pass strategy:

1. **Original name first** — try the full model ID against the external registry and the bundled capability table.
2. **Normalized bare name** — if step 1 fails, strip the last `/`-segment (everything up to and including the final `/`) to obtain the bare model name, then retry both the registry and the bundled table.

This preserves model-specific registry entries that include provider prefixes (e.g. `openrouter/gpt-4o`) while also matching bare model names embedded in a prefixed identifier.

### Capability Table

The bundled table (in `internal/api/model_caps.go`) maps model name prefixes to maximum output tokens. It is the single source of truth — providers do not maintain their own defaults. The table covers all known model families (Claude, GPT, Gemini, DeepSeek, o-series) with current values verified as of June 2026. Unknown models receive the conservative fallback of 16384 tokens.

### Warning Channel

Clamp events emit structured WARN log lines via the existing logger. The log contains `model`, `override`, `resolved`, and `reason` fields. In streaming mode, the warning is also surfaced as a `system` event in the stream-json protocol.

### Relationship to MaxTokensError

The `categorizeMaxTokensError` function populates `MaxOutputTokens` from the same capability table, making the field trustworthy for callers receiving either a `CategoryOutputCapHit` or `CategoryContextExhausted` error. Since pre-request clamping prevents configuration overshoot from reaching the API, a `MaxTokensError` represents a legitimate output-cap hit — the caller configured correctly, and the model simply filled its budget.

## External Model Registry

Jenny fetches a community-maintained model registry (`aidy-models`) to keep capability, pricing, and context-window data current without hand-maintenance. The registry is consulted at lookup time and falls back gracefully to the bundled table when unavailable.

### Resolution Order

Capability and pricing lookups consult three sources, in order:

1. **User config.json override** — `config.json`'s `models` key, a sparse patch keyed by model ID. Supports partial field-level overrides for `maxOutput`, `contextWindow`, `pricing` (field-by-field merge), `modalities`, and `abilities`. A malformed block emits a WARN log and is treated as empty.
2. **External registry snapshot** — `~/.jenny/models.json`, fetched from `aidy-models` on startup. Contains 6700+ models across 190+ providers.
3. **Bundled defaults** — the capability table in `internal/api/model_caps.go` and the pricing table in `internal/agent/cost.go`.

### Storage

| File | Role | Writer |
|------|------|--------|
| `~/.jenny/models.json` | Upstream registry snapshot (verbatim JSON, ~5.5MB) | jenny (fetch on startup) |
| `~/.jenny/meta.json` | Fetch metadata (fetchedAt, ETag, schemaVersion) and exchange rates | jenny |
| `config.json` → `models` key | User-supplied field overrides on individual models | user (handwritten) |

`models.json` and `meta.json` are separate files so their lifecycles do not collide: the fetch goroutine rewrites them; the user rewrites `config.json`. A user editing their override block does not race with a fetch.

### Fetch Strategy

- On startup, jenny reads `meta.json`. If the cache is **missing** or **older than 24 hours**, a background goroutine fetches the registry with a 3-second soft timeout.
- The fetch honors `If-None-Match` (ETag); a 304 response is a no-op.
- `--refresh-registry` triggers a synchronous, blocking fetch.
- `--offline` skips all fetch attempts.
- On HTTP error or timeout, the existing cache is preserved. A corrupt cache file is renamed to `.broken` and treated as missing.
- The cached file is parsed lazily on first lookup, then memoized in memory.

### Graceful Degradation

Every subsystem that consults the registry continues to work when it is unavailable:
- **Capability resolution** falls back to the bundled table.
- **Cost tracking** falls back to hard-coded per-model pricing (or zero cost for unknown models).
- **Future consumers** (protocol detection) fall back to environment-variable-based provider selection.

The worst case is identical to the behavior before the registry existed.