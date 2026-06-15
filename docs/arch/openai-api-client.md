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

## Streaming

Streaming uses Server-Sent Events (SSE). Both Chat and Responses API implementations yield partial content blocks as they arrive from the network without buffering the full response.

## Acceptance Criteria

- **AC1:** Selected automatically when `OPENAI_BASE_URL` is present.
- **AC2:** Correctly maps Anthropic-style tool results to OpenAI `role: tool` messages.
- **AC3:** Supports real SSE streaming without full-body buffering.
- **AC4:** Supports `OPENAI_WIRE_API=responses` with optimization for `instructions` field.
- **AC5:** Handles multi-block system prompts by sending them as separate system messages.

