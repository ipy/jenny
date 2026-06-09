---
title: Stream-JSON Output Protocol
slug: stream-json
priority: P0
status: done
spec: complete
code: done
defer_to: P3
package: internal/cli, internal/agent
gaps:
  []
depends_on:
  - cli
  - agent-loop
  - sse-streaming
---
# Stream-JSON Output Protocol

## Overview

Headless Jenny runs emit **NDJSON** (newline-delimited JSON) on **stdout only**. Each line is one JSON object. Debug and logs go to **stderr**. This protocol must be fully compatible with SDK consumers that parse agent activity.

## Requirements

- Requires non-interactive mode (`-p` / `--print` or positional prompt).
- Install stdout guard when `--output-format stream-json` to prevent non-JSON leakage to stdout.
- JSON stringify must escape U+2028/U+2029 for NDJSON safety.

## Common Fields

Every event carries these fields in declaration order:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Event type identifier |
| `session_id` | string | Stable session identifier |
| `parent_tool_use_id` | string\|null | Parent tool use ID for nested events; `null` for top-level |
| `uuid` | string | Unique event identifier |

## Message Sequence (Typical Turn)

```
1. system/init          (once per process start or resume)
2. stream_request_start (before each API iteration; jenny extension)
3. stream_event        (raw SSE deltas when --include-partial-messages)
4. assistant           (aggregated final message after content_block_stop)
5. tool_call/started   (before tool execution)
6. tool_call/completed (after tool execution)
7. user (aggregated tool result batch after last tool_call completed)
8. [repeat 2-7 for each turn]
…
N. result              (terminal line, always last)
```

## Message Types

### `system` / `init`

First line after startup:

```json
{
  "type": "system",
  "subtype": "init",
  "cwd": "/path/to/project",
  "session_id": "sess_…",
  "tools": ["Read", "Write", "Bash", …],
  "mcp_servers": [{ "name": "…", "status": "connected" }],
  "model": "deepseek-v4-flash",
  "permissionMode": "default",
  "uuid": "…"
}
```

Optional fields: `slash_commands`, `skills`, `plugins`.

### `stream_request_start`

Emitted before each API iteration. **This is a jenny extension** — not part of the headless-agent reference format.

```json
{
  "type": "stream_request_start",
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…"
}
```

### `assistant` (Aggregated)

Wraps the complete API-shaped assistant message after `content_block_stop`. Emitted **once per turn**, after all content blocks are received:

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "thinking", "thinking": "…", "signature": "…" },
      { "type": "text", "text": "…" },
      { "type": "tool_use", "id": "toolu_…", "name": "Read", "input": { "file_path": "…" } }
    ]
  },
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…"
}
```

### `user` (Aggregated Tool Results)

Emitted after the last `tool_call` `completed` event in a batch, before the next `stream_request_start`:

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_…",
        "content": "…",
        "is_error": false
      }
    ]
  },
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…"
}
```

### Flat `tool_use` (legacy headless parsers)

When emitting flat tool events (not full assistant wrapper), use **`parameters`**, not `tool_input`:

```json
{
  "type": "tool_use",
  "tool_name": "Read",
  "parameters": { "file_path": "foo.go" },
  "session_id": "sess_…"
}
```

### `tool_call` started / completed

```json
{ "type": "tool_call", "subtype": "started", "tool_name": "Bash", "tool_use_id": "…", "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
{ "type": "tool_call", "subtype": "completed", "tool_use_id": "…", "is_error": false, "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
```

### `stream_event` (partial messages)

When `--include-partial-messages` is set, forward raw SSE events:

```json
{
  "type": "stream_event",
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…",
  "event": { "type": "content_block_delta", "delta": { "type": "text_delta", "text": "Hel" } }
}
```

Requires live SSE streaming from API (see [`sse-streaming.md`](./sse-streaming.md)).

### `system` / `compact_boundary`

Emitted after context compaction:

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "compact_metadata": {
    "trigger": "auto",
    "pre_tokens": 180000,
    "preserved_segment": "…"
  }
}
```

### `result` (Terminal)

Always the last line on successful run. Carries `total_cost_usd` only here.

```json
{
  "type": "result",
  "subtype": "success",
  "result": "Final assistant text",
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…",
  "usage": {
    "input_tokens": 100,
    "output_tokens": 50,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0
  },
  "total_cost_usd": 0.001,
  "duration_ms": 3000,
  "duration_api_ms": 2800,
  "num_turns": 2,
  "stop_reason": "end_turn",
  "modelUsage": {
    "deepseek-v4-flash": {
      "inputTokens": 100,
      "outputTokens": 50,
      "cacheReadInputTokens": 0,
      "cacheCreationInputTokens": 0,
      "webSearchRequests": 0,
      "costUSD": 0.001,
      "contextWindow": 64000,
      "maxOutputTokens": 8000
    }
  }
}
```

Error subtypes: `error`, `error_max_tokens`, `error_max_turns`, `error_budget`.

#### `error_max_tokens` shape

When output is capped due to `max_tokens`, the `result` event includes `error_max_tokens`:

```json
{
  "type": "result",
  "subtype": "error_max_tokens",
  "result": "max tokens reached: output_cap",
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…",
  "usage": { "input_tokens": 100, "output_tokens": 50, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0 },
  "total_cost_usd": 0.001,
  "duration_ms": 3000,
  "duration_api_ms": 2800,
  "num_turns": 2,
  "stop_reason": "max_tokens",
  "error_max_tokens": {
    "category": "output_cap",
    "output_tokens": 50,
    "max_output_tokens": 50,
    "input_tokens": 100,
    "threshold": 150000
  },
  "modelUsage": { … }
}
```

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Log/debug output | stderr only; never stdout in stream-json mode |
| Empty final text | `result` with empty string, still emit usage |
| Tool error | `is_error: true` on tool_result; still continue to result |
| Interrupt | Synthetic error tool_results for pending tool_use |
| Resume | Same `session_id` in all lines |

## Acceptance Criteria

- **AC1:** Every stdout line valid JSON when format is stream-json.
- **AC2:** Flat tool_use events use `parameters` key.
- **AC3:** Terminal line is always `type: result` with usage snake_case fields.
- **AC4:** `session_id` consistent across init, turns, and result.
- **AC5:** Partial events only when `--include-partial-messages` and SSE enabled.
- **AC6:** `total_cost_usd` appears exactly once — on the terminal `result` event.
- **AC7:** Every event carries `parent_tool_use_id` (null for top-level).
- **AC8:** Field order matches reference format: `type`, then `event|message|payload`, then `session_id`, `parent_tool_use_id`, `uuid`, then remaining fields.

## Related

- CLI flags: [`cli.md`](./cli.md)
- Cost fields: [`cost-tracking.md`](./cost-tracking.md)
- SSE dependency: [`sse-streaming.md`](./sse-streaming.md)
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_…",
        "content": "…",
        "is_error": false
      }
    ]
  },
  "session_id": "sess_…"
}
```

### `tool_call` started / completed

Headless activity parsers may receive:

```json
{ "type": "tool_call", "subtype": "started", "tool_name": "Bash", "tool_use_id": "…" }
{ "type": "tool_call", "subtype": "completed", "tool_use_id": "…", "is_error": false }
```

### `stream_event` (partial messages)

When `--include-partial-messages` is set, forward raw SSE events:

```json
{
  "type": "stream_event",
  "event": { "type": "content_block_delta", "delta": { "type": "text_delta", "text": "Hel" } }
}
```

Requires live SSE streaming from API (see [`sse-streaming.md`](./sse-streaming.md)).

### `system` / `compact_boundary`

Emitted after context compaction:

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "compact_metadata": {
    "trigger": "auto",
    "pre_tokens": 180000,
    "preserved_segment": "…"
  }
}
```

### `result` (terminal)

Always the last line on successful run:

```json
{
  "type": "result",
  "subtype": "success",
  "result": "Final assistant text",
  "session_id": "sess_…",
  "usage": {
    "input_tokens": 100,
    "output_tokens": 50,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0
  },
  "total_cost_usd": 0.001,
  "duration_ms": 3000,
  "num_turns": 2,
  "stop_reason": "end_turn"
}
```

Error subtypes: `error`, `error_max_turns`, `error_budget`, etc.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Log/debug output | stderr only; never stdout in stream-json mode |
| Empty final text | `result` with empty string, still emit usage |
| Tool error | `is_error: true` on tool_result; still continue to result |
| Interrupt | Synthetic error tool_results for pending tool_use |
| Resume | Same `session_id` in all lines |

## Acceptance Criteria

- **AC1:** Every stdout line valid JSON when format is stream-json.
- **AC2:** Flat tool_use events use `parameters` key.
- **AC3:** Terminal line is always `type: result` with usage snake_case fields.
- **AC4:** `session_id` consistent across init, turns, and result.
- **AC5:** Partial events only when `--include-partial-messages` and SSE enabled.

## Related

- CLI flags: [`cli.md`](./cli.md)
- Cost fields: [`cost-tracking.md`](./cost-tracking.md)
- SSE dependency: [`sse-streaming.md`](./sse-streaming.md)
