---
title: SDK Input Protocol
slug: sdk-input-protocol
priority: P0
status: partial
spec: complete
code: not_started
package: internal/cli
gaps:
  - Full implementation
depends_on:
  - cli
  - stream-json
---
# SDK Input Protocol

## Overview

When `--input-format stream-json` is set, jenny reads NDJSON from stdin and writes NDJSON to stdout. This enables jenny to be used as a drop-in replacement for `ClaudeCodeExecutable` in the `@anthropic-ai/claude-agent-sdk`.

## Activation

The protocol activates when:
1. `--input-format stream-json` is passed (always passed by the SDK)
2. `--output-format stream-json` is also passed (always passed by the SDK)

When active, the prompt requirement from CLI args is relaxed — the prompt arrives via stdin NDJSON instead.

## Stdin Message Types

Each stdin line is one JSON object. Empty lines are ignored.

### `user` — User prompt message

```json
{
  "type": "user",
  "session_id": "",
  "message": {
    "role": "user",
    "content": [{"type": "text", "text": "Hello, Claude!"}]
  },
  "parent_tool_use_id": null
}
```

The first `user` message provides the prompt. The `text` field from the first `text` content block is extracted as the prompt string.

### `control_request` — SDK-initiated control command

```json
{
  "request_id": "<id>",
  "type": "control_request",
  "request": {
    "subtype": "<subtype>",
    ...
  }
}
```

Subtypes:
- `initialize` — Register hooks, MCP servers, system prompt overrides. Sent once before agent loop starts.
- `interrupt` — Abort current turn.

### `control_response` — SDK reply to jenny-initiated requests

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<matching-id>",
    "response": { ... }
  }
}
```

Sent by the SDK in response to jenny's `control_request` messages on stdout (e.g., `can_use_tool`, `hook_callback`).

### `keep_alive` — Heartbeat

```json
{"type": "keep_alive"}
```

Ignored.

## Stdout Control Messages

In addition to the standard stream-json events, jenny emits control messages on stdout.

### `control_request` — jenny asks SDK for a decision

```json
{
  "type": "control_request",
  "request_id": "<uuid>",
  "request": {
    "subtype": "can_use_tool",
    "tool_name": "Bash",
    "input": {"command": "ls"},
    "tool_use_id": "<uuid>"
  }
}
```

Subtypes:
- `can_use_tool` — Tool needs permission approval (when `--permission-prompt-tool stdio`).
- `hook_callback` — An SDK-registered hook fires (PreToolUse, PostToolUse, etc.).

### `control_response` — jenny replies to SDK control_request

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<matching-id>",
    "response": { ... }
  }
}
```

## Initialize Handshake

The SDK sends an `initialize` control_request on stdin immediately after spawning the process. Jenny responds with a `control_response` on stdout before processing user messages.

Initialize request:
```json
{
  "request_id": "<id>",
  "type": "control_request",
  "request": {
    "subtype": "initialize",
    "hooks": { ... },
    "sdkMcpServers": [],
    "jsonSchema": null,
    "systemPrompt": null,
    "appendSystemPrompt": null,
    "agents": null
  }
}
```

Initialize response:
```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<matching-id>",
    "response": {}
  }
}
```

### Initialize Config Fields

Jenny applies the following fields from the initialize request to the agent configuration:

- `hooks` — Register SDK hooks (PreToolUse, etc.) for callback protocol.
- `systemPrompt` — When non-empty, replaces the default system prompt entirely.
- `appendSystemPrompt` — When non-empty, appended after the assembled system prompt.

These fields override any CLI-provided system prompt flags.

## Hook Callback Protocol

When the SDK registers hooks via `initialize`, jenny sends `hook_callback` control_requests on stdout before/after tool execution. The SDK executes the callback and responds on stdin.

Hook callback request:
```json
{
  "type": "control_request",
  "request_id": "<uuid>",
  "request": {
    "subtype": "hook_callback",
    "callback_id": "hook_0",
    "input": {
      "hook_event_name": "PreToolUse",
      "tool_name": "Write",
      "tool_input": { ... },
      "tool_use_id": "...",
      "session_id": "...",
      "cwd": "..."
    },
    "tool_use_id": "..."
  }
}
```

Hook callback response (from SDK):
```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<matching-id>",
    "response": {
      "decision": "approve",
      "hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "permissionDecision": "allow"
      }
    }
  }
}
```

A `block` decision or `permissionDecision: "deny"` prevents tool execution.

## Startup Sequence

1. SDK spawns jenny with `--output-format stream-json --verbose --input-format stream-json [other flags]`
2. SDK writes user message to stdin
3. SDK writes `initialize` control_request to stdin
4. Jenny reads stdin, processes both messages
5. Jenny responds to `initialize` with control_response on stdout
6. Jenny emits `system/init` on stdout
7. Jenny runs the agent loop, emitting stream-json events
8. Jenny emits terminal `result` event
9. SDK reads `result`, closes stdin

## Acceptance Criteria

- **AC1:** `--input-format stream-json` accepted without error.
- **AC2:** Prompt extracted from stdin `user` message when `--input-format stream-json`.
- **AC3:** `initialize` control_request receives a `control_response` on stdout.
- **AC4:** Unknown flags from SDK (e.g., `--betas`, `--add-dir`) accepted without error.
- **AC5:** `hook_callback` control_request triggers SDK hook execution and waits for response.
- **AC6:** All existing stream-json output remains unchanged.

## Related

- CLI flags: [cli.md](./cli.md)
- Stream-json output: [stream-json.md](./stream-json.md)
