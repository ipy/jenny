---
title: SSE Streaming from API
slug: sse-streaming
priority: P0
status: draft
spec: partial
code: done
package: internal/api
gaps:
  - "Idle timeout fallback architecturally intended but has timing issue with scanner.Next() blocking"
depends_on:
  - anthropic-api-client
  - provider-architecture
---
# SSE Streaming from API

## Overview

Primary API path uses server-sent events. Partial text accumulates per content block; on failure, fall back to non-streaming with bounded timeout.

## Stream Loop

- Request with `stream: true`.
- Use raw message stream events (avoid O(n²) partial JSON parser).
- Yield `{ type: stream_event, event: part }` for each SSE event.
- On `content_block_stop`: yield completed assistant block.
- On `message_delta`: update last assistant usage and stop_reason in place.

Idle watchdog: abort if no chunks within configured timeout.

## include_partial_messages

When enabled: forward raw `stream_event` to consumer.

When disabled: only completed assistant/user messages.

Partial assistant text for stream-json depends on this flag **and** live SSE.

**Note:** The API layer always emits raw `stream_event` blocks. Filtering based on `--include-partial-messages` CLI flag happens at the engine layer (`IncludePartial` in `StreamConfig`), not the API layer.

## Non-Streaming Fallback

Trigger on: stream exception, idle timeout, incomplete stream (no message_start or no stop_reason). Permanent errors (e.g., 404) bypass fallback (`IsPermanent = true`).

Fallback:

- Non-streaming API call; timeout ~5 min max.
- `onStreamingFallback`: tombstone partial assistant messages, discard streaming tool executor, clear pending tool_use IDs.
- Count streaming 529 toward 529 budget.
- Fallback is disabled by passing `nil` for `onStreamingFallback`.
- Skip fallback when parent context is already cancelled (e.g., after Ctrl+C).

**Multi-provider note:** The SSE event model is Anthropic-canonical. All providers (OpenAI, GenAI) translate their native streaming events into Anthropic-compatible `stream_event` structs. See [`provider-architecture.md`](./provider-architecture.md).

## Resource Cleanup

Always cancel response body on exit. Stream reader is context-aware: when the parent context is cancelled (e.g., Ctrl+C), the stream loop exits within one read cycle rather than waiting for the idle watchdog timeout.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| User abort (detected via string heuristic `isUserAbortError`) | Rethrow unless tool interrupt variant |
| Structured output turn 2 empty | Valid with stop_reason; no false incomplete |
| Fallback | Do not reuse partial tool_use IDs |

## Acceptance Criteria

- **AC1:** SSE default; text arrives via deltas.
- **AC2:** Fallback completes via non-streaming on failure.
- **AC4:** Partial events only when flag enabled.
- **AC5:** Partial stream tombstoned before fallback retry.
