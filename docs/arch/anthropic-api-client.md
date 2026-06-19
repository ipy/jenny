---
title: Anthropic API Client
slug: anthropic-api-client
priority: P0
status: done
spec: complete
code: done
package: internal/api
gaps:
  - doc scoped to Anthropic provider; see provider-architecture.md for multi-provider design
defer_to: P3
depends_on:
  - system-prompt
  - message-normalization
  - sse-streaming
---
# Anthropic API Client

## Overview

Jenny's API client is a multi-provider facade (`internal/api`) with a `Provider` interface implemented by Anthropic, OpenAI Chat, OpenAI Responses, and GenAI backends. This doc focuses on the Anthropic provider specifics. It handles message shape, system prompt placement, tool pairing, media validation, and provider-agnostic normalization. See [provider-architecture.md](./provider-architecture.md) and [multi-provider-routing.md](./multi-provider-routing.md) for the full multi-provider design.

## System Prompt

System prompt is a **top-level request parameter**, not a `role: system` message in `messages[]`.

```json
{
  "model": "…",
  "system": [
    { "type": "text", "text": "<block1>" },
    { "type": "text", "text": "<block2>" },
    { "type": "text", "text": "<block3>" },
    { "type": "text", "text": "<block4>", "cache_control": { "type": "ephemeral" } }
  ],
  "messages": [ … ]
}
```

The system prompt consists of multiple text blocks. Cache control is placed on the final block of the static system prompt to maximize cache hits for the prefix (see [`system-prompt.md`](./system-prompt.md)).

## Tool Use / Tool Result Pairing

1. Assistant message with `tool_use` blocks must be sent **before** user message with matching `tool_result` blocks.
2. Each `tool_result.tool_use_id` must match a preceding `tool_use.id`.
3. On interrupt, synthesize error `tool_result` for every pending `tool_use`.

Normalization before send (see [`message-normalization.md`](./message-normalization.md)):

- Insert synthetic error results for missing IDs.
- Strip orphaned tool_results.
- Ensure assistant message includes full `tool_use` block (not just text).

## Thinking Blocks

Assistant messages with extended thinking:

- Thinking block must **not** be the last block in a message sent to API.
- Preserve thinking across assistant → tool_result → assistant turns.
- Strip trailing thinking from last assistant before request.

## Image and Media Validation

Pre-request validation:

| Limit | Value |
|-------|-------|
| Max media items per request | 100 |
| Max base64 size per image | 5 MB |

`ValidateMessagesMedia()` runs before send; fail fast with actionable error.

## Oversize Media Error Mapping

Map API 400/413 responses to user-facing strings:

| Condition | Message pattern |
|-----------|-----------------|
| Image too large (pre or post) | Actionable resize/remove guidance |
| Too many images dimensions | Compact or remove images guidance |
| Request too large (413) | Reduce context guidance |

Note: PDF-specific error mapping (page limits, password-protected, invalid PDF) is not implemented in the current codebase.

## Streaming vs Non-Streaming

SSE streaming is the default path with non-streaming fallback on incomplete streams (see [`sse-streaming.md`](./sse-streaming.md)).

## Cache Headers

Prompt cache: optional cache control breakpoints on system blocks and stable prefixes. Track `cache_read_input_tokens` and `cache_creation_input_tokens` in usage.

The `anthropic-beta: prompt-caching-2024-07-31` header is sent on all requests. The tool definitions array is cached as a stable prefix by setting `cache_control` on the last tool entry. Additionally, `markLastMessageForCaching` sets `cache_control: ephemeral` on the last content block of the last non-empty message, enabling prefix caching to cover the entire conversation history.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Empty assistant after tool strip | Insert `[Tool use interrupted]` text (implemented in `internal/agent/normalize.go`, not `internal/api`) |
| Consecutive same-role messages | `MergeConsecutiveSameRole()` in `NormalizeMessages()` — universal pass for all providers, not Bedrock-specific |
| Invalid tool JSON in tool_use | Silently replaced with empty map (`make(map[string]any)`) in both streaming (`finalizeToolInput`) and non-streaming paths |
| Max output tokens exceeded | Returns `MaxTokensError` to signal the caller for structured error reporting — no automatic retry with adjusted max_tokens |

## Acceptance Criteria

- **AC1:** System prompt never sent as user/assistant message.
- **AC2:** Every tool_use has matching tool_result in following user message.
- **AC3:** Image count ≤ 100; each base64 ≤ 5 MB before send.
- **AC4:** Media errors map to specific user-facing strings.
- **AC5:** Trailing thinking stripped from last assistant block.

## Provider Compatibility

Tool serialization uses **universal normalization** — all fixes apply unconditionally to every provider, eliminating provider-specific code paths.

### Universal Normalization

The following passes are applied universally via `NormalizeMessages` (provider-agnostic):

| Pass | Trigger | Description |
|------|---------|-------------|
| Merge Consecutive Same-Role | Adjacent messages with same role | Merges consecutive same-role messages into one |
| Credential-Bound Artifact Stripping | `redacted_thinking` with key mismatch | Strips redacted thinking blocks when API key changes |
| Content Block Validation | Every message | Validates content blocks are well-formed |
| Thinking-Orphan Dropping | Orphaned thinking messages | Drops messages that only contain thinking blocks without subsequent content |
| Trailing Thinking Stripping | Last assistant message | Strips trailing thinking block from last assistant before request |
| Empty Schema Placeholder | Tools with empty `input_schema.properties` | Injects `__arg__: {type: string}` placeholder |
| Tool Result Dedup | Every `tool_result` block | Deduplicates by `tool_use_id` (last-writer-wins) |

For full details on the normalization architecture, see [`universal-normalization-architecture.md`](./universal-normalization-architecture.md).

## Related

- Message normalization: [`message-normalization.md`](./message-normalization.md)
- Universal normalization architecture: [`universal-normalization-architecture.md`](./universal-normalization-architecture.md)
- Agent loop: [`agent-loop.md`](./agent-loop.md)
