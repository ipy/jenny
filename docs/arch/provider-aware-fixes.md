---
title: Provider-Aware Fixes → Universal Normalization
slug: provider-aware-fixes
priority: P2
status: done
spec: complete
code: done
package: internal/api
gaps: []
depends_on:
  - universal-normalization-architecture
  - anthropic-api-client
  - openai-api-client
---
# Provider-Aware Fixes → Universal Normalization

## Context

This document previously described provider-specific shims gated on URL-based provider detection. The architecture has since been refactored to use **universal normalization** — all fixes now apply unconditionally to every provider, eliminating provider-specific code paths.

See [`universal-normalization-architecture.md`](./universal-normalization-architecture.md) for the current architecture.

## Normalization Pass Map

`NormalizeMessages()` applies the following passes unconditionally in order:

| Pass | Function | Description |
|------|----------|-------------|
| Tool Schema Stabilization | `NormalizeTools` | Injects `__arg__: {type: string}` for empty `input_schema.properties`. Strips experimental beta fields (`defer_loading`, `cache_control`, `eager_input_streaming`) when `DisableExperimentalBetas` is set. |
| Tool Result Flattening | `flattenToolResultContent` | Universal hook for tool result content normalization (currently a pass-through). |
| Merge Consecutive Same-Role | `MergeConsecutiveSameRole` | Collapses adjacent messages with the same role (Bedrock compatibility). |
| Credential-Bound Artifact Strip | `stripCredentialBoundArtifacts` | Strips `redacted_thinking` blocks when session is resumed with a different API key (`OriginalAPIKey` mismatch). |
| Tool Result Dedup | `deduplicateToolResults` | Deduplicated by `tool_use_id` (last-writer-wins). |
| Content Block Validation | `validateContentBlocks` | AC1: strip whitespace-only text blocks. AC2: insert `[No content]` for empty non-final assistant messages. AC3: strip trailing thinking/redacted_thinking blocks. AC4: drop messages containing only thinking blocks. |

## Why Universal?

The previous URL-based detection (`providerFromBaseURL()`) was fragile — it relied on substring matching in the base URL and required separate code paths for each provider. The universal approach:

1. Applies fixes unconditionally to all outgoing payloads
2. Eliminates provider-specific branching in the serialization path
3. Guarantees compatibility across all API providers without detection

## Migration Notes

- `providerFromBaseURL()` has been removed
- Tool serialization no longer branches on provider type
- `NormalizeMessages` in `internal/api/` is the single gateway before JSON serialization
- All existing tests pass without modification

## Model-Specific Request Transformers

Some backends (currently DeepSeek) require provider-specific request fields that vary by wire format. A request transformer hook is applied to outgoing requests when the resolved model is detected as DeepSeek. The transformer dispatches on the request body type and emits the appropriate field set for that backend:

| Wire format | Body type | DeepSeek-specific field |
|-------------|-----------|-------------------------|
| Chat | `OpenAIRequest` | `extra_body.thinking.type = "enabled"` when reasoning effort is set |
| Responses | `OpenAIResponsesRequest` | `extra_body.thinking.type = "enabled"` when `reasoning_config.effort` is set |
| Anthropic-compatible | `AnthropicRequest` | `output_config.effort` mapped from the thinking budget tokens |

The transformer is attached via `SetRequestTransformer` on the provider after the provider is constructed. Providers that do not implement the interface (e.g., the GenAI/Gemini provider) are unaffected.

## Model Capability Lookup Precedence

Capability resolution for max output tokens, context window, vision, and native search follows a four-step precedence:

1. **External model registry** (original name) — config-loaded JSON registry takes precedence when present.
2. **External model registry** (normalized bare name) — same registry consulted after stripping any `provider/` prefix so that `deepseek-anthropic/deepseek-v4-pro` resolves to the same entry as `deepseek-v4-pro`.
3. **Bundled capability table** (normalized bare name only) — provider-prefixed model IDs are normalized before pattern matching against the bundled table. This prevents broad catch-all patterns (e.g., `deepseek`) from masking more specific entries (e.g., `deepseek-v4-pro`).
4. **Conservative fallback** — unknown models receive the conservative default (16,384 max output tokens for `ResolveMaxTokens`, 200,000 context window for `modelContextWindowLookup`, vision = true, native search = true).

## References

- [`universal-normalization-architecture.md`](./universal-normalization-architecture.md) — Current architecture source of truth
- [`anthropic-api-client.md`](./anthropic-api-client.md) — Provider Compatibility section updated
- [`openai-api-client.md`](./openai-api-client.md) — DeepSeek thinking mode and Responses API behavior
