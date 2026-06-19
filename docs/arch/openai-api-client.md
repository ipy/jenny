---
title: OpenAI API Client
slug: openai-api-client
priority: P0
status: done
spec: complete
code: done
package: internal/api
gaps: []
depends_on:
  - anthropic-api-client
  - provider-architecture
---

# OpenAI API Client

## Overview

The OpenAI provider implements the `Provider` interface using the OpenAI Chat Completions API. It is designed to be wire-compatible with OpenAI-compatible proxies and backends.

## Selection Logic

The OpenAI provider is selected if the `OPENAI_BASE_URL` environment variable is set. It takes precedence over the default Anthropic provider.

## Configuration

| Environment Variable | Description | Default |
|----------------------|-------------|---------|
| `OPENAI_BASE_URL` | Base URL for the API (e.g., `https://api.openai.com/v1`) | (Required for selection) |
| `OPENAI_API_KEY` | API key for authentication | (Required) |
| `OPENAI_DEFAULT_MODEL`| Model name to use if not specified | (Required) |
| `OPENAI_WIRE_API` | Wire protocol version (`chat` or `responses`) | `chat` |

## Responses API Optimization

When `OPENAI_WIRE_API=responses` is used, the provider optimizes system prompt placement:

1.  **Instructions Field**: The first (most stable) block of the system prompt (containing identity and core tasks) is sent in the top-level `instructions` field.
2.  **System Messages**: Subsequent blocks (CWD, Memory, Git Status, Date) are added as `role: system` messages at the start of the `input` list.

This separation ensures that the core identity remains stable in the dedicated field while project-specific and dynamic context is provided as structured messages.

## Mapping

### Roles
- `user` -> `user`
- `assistant` -> `assistant`
- `system` -> `system` (passed as the first message(s) in the array)
- `tool` -> `tool` (OpenAI uses `role: tool` with `tool_call_id`)

### Stop Reasons
| OpenAI `finish_reason` | Jenny `StopReason` |
|------------------------|-------------------|
| `stop` | `end_turn` |
| `tool_calls` | `tool_use` |
| `length` | `max_tokens` |
| (other) | (passthrough) |

## Normalization

The OpenAI provider utilizes the `NormalizeMessages` pipeline to ensure payload compatibility, specifically:
- **Tool Result Dedup:** Ensures `tool_call_id` matches.
- **Role Alternation:** Merges consecutive messages of the same role.

## Native Web Search Support

The `SupportsNativeSearch()` method determines whether web search tools are available:

- **Chat API provider**: Returns `true` only for known OpenAI model prefixes (`gpt-`, `o3`, `o4`, `chatgpt-`). Returns `false` for third-party models routed through OpenAI-compatible APIs (DeepSeek, Moonshot, etc.).
- **Responses API provider**: Always returns `true`.

## DeepSeek Thinking Mode

When thinking effort is configured and the model is detected as a DeepSeek model (via `isDSModel()`), the provider injects `extra_body: {"thinking": {"type": "enabled"}}` into both streaming and non-streaming requests. This enables DeepSeek's native chain-of-thought reasoning.

## Thinking Content Round-Trip

Both providers preserve thinking/reasoning content across multi-turn conversations:

- **Chat API**: Round-trips thinking as `reasoning_content` / `ReasoningContent` fields on assistant messages.
- **Responses API**: Emits reasoning as `reasoning` / `summary` item types in the `output` array.

On input, previously-generated thinking blocks are re-sent to the model to maintain context continuity.

## Streaming

Streaming uses Server-Sent Events (SSE). Both Chat and Responses API implementations yield partial content blocks as they arrive from the network without buffering the full response.

Stream events are normalized to Anthropic-compatible events (`message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`) for compatibility with the shared event consumption layer. See [`provider-architecture.md`](./provider-architecture.md) for the cross-provider normalization design.

## Acceptance Criteria

- **AC1:** Selected automatically when `OPENAI_BASE_URL` is present.
- **AC2:** Correctly maps Anthropic-style tool results to OpenAI `role: tool` messages.
- **AC3:** Supports real SSE streaming without full-body buffering.
- **AC4:** Supports `OPENAI_WIRE_API=responses` with optimization for `instructions` field.
- **AC5:** Handles multi-block system prompts by sending them as separate system messages.

