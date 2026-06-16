---
title: MCP Client
slug: mcp-client
priority: P0
status: partial
spec: complete
code: done
package: internal/mcp
implemented:
  - "Stdio transport (subprocess JSON-RPC)"
  - "HTTP Streamable transport (MCP spec 2025-03-26)"
  - "Transport interface abstraction"
  - "Session management (Mcp-Session-Id)"
  - "Auto re-initialization on session expiry (HTTP 404)"
  - "Custom headers from config"
  - "Configurable timeouts (MCP_HTTP_REQUEST_TIMEOUT)"
  - "SSE response parsing with request ID matching"
  - "Content truncation (MCP_MAX_OUTPUT_CHARS)"
  - "Binary tool results persisted to disk"
  - "Cursor-based pagination for tools/list and resources/list"
  - "Tool naming normalization with empty-string fallback"
  - "Resource cache with generation-based invalidation"
  - "ConnectAll supports both stdio and HTTP"
  - "Client capabilities (roots/listChanged, sampling)"
  - "Resource list invalidation (notifications/resources/list_changed)"
  - "Server logging (notifications/message)"
  - "Prompts (prompts/list, prompts/get)"
  - "Resource templates (resources/templates/list)"
  - "Progress events (notifications/progress)"
gaps:
  - "OAuth 2.1 authorization not implemented"
  - "Resource subscriptions (resources/subscribe) not implemented"
  - "Icons metadata not supported"
  - "Tasks (experimental) not implemented"
defer_to: P4
depends_on:
  - mcp-config
---
# MCP Client

## Overview

Jenny implements an MCP client that connects to configured servers, exposes their tools to the model, and handles auth, transport, and result size limits in headless mode.

## Transports

| Transport | Use case | Config trigger |
|-----------|----------|----------------|
| `stdio` | Local subprocess; stdin/stdout JSON-RPC | `command` field present |
| `http` | Streamable HTTP (MCP spec 2025-03-26+) | `url` field present, no `command` |

Connection lifecycle: connect → initialize → list tools/resources → ready.

### Transport Selection Logic

```
if def.Command != "" → stdio transport
else if def.URL != "" → HTTP (Streamable HTTP) transport
else → skip (invalid config)
```

### Stdio Transport

Spawn subprocess, newline-delimited JSON-RPC 2.0 over stdin/stdout. Existing behavior unchanged.

### HTTP Transport (Streamable HTTP)

Per MCP spec 2025-03-26. Single HTTP endpoint, JSON-RPC over POST.

**Request requirements:**
- POST JSON-RPC messages to the server URL
- `Content-Type: application/json`
- `Accept: application/json, text/event-stream`
- Include `Mcp-Session-Id` header after initialization (if server provides one)

**Response handling:**
- `Content-Type: application/json` → parse single JSON-RPC response
- `Content-Type: text/event-stream` → parse SSE stream, extract JSON-RPC messages from `data:` lines with `event: message`
- HTTP 202 Accepted → notification/response acknowledged, no body
- HTTP 404 with session ID → session expired, must re-initialize

**Session management:**
- Server MAY return `Mcp-Session-Id` in `InitializeResult` response headers
- Client MUST include this header in all subsequent requests
- On HTTP 404, discard session and re-initialize

**Notifications (client → server):**
- POST notification, expect HTTP 202 Accepted

**Custom headers:**
- `headers` from config merged into every request (e.g., `Authorization: Bearer ...`)

**Timeouts:**
- Request timeout: 120s default (tool calls may be long-running)
- Configurable via environment: `MCP_HTTP_REQUEST_TIMEOUT` (value in seconds)

## OAuth and 401 Handling (Not Implemented — P4)

Planned behavior for when OAuth is implemented:

1. Attempt token refresh via stored OAuth credentials.
2. Retry request once with refreshed token.
3. If refresh fails, mark server status `needs-auth` and surface error to operator (no interactive prompt in headless mode).

Currently, static auth tokens can be passed via `headers` config field (e.g., `"Authorization": "Bearer <token>"`).

## Tool Naming

MCP tools are exposed to the model with normalized names:

```
mcp__<normalized_server>__<normalized_tool>
```

Normalization: lowercase, non-alphanumeric → underscore, collapse repeats, trim. If normalization produces an empty string, falls back to `"unnamed"`.

Example: server `My Server`, tool `List Files` → `mcp__my_server__list_files`

## Binary Results

Binary MCP content must **not** be inlined as base64 in tool_result text.

Flow:

1. Decode blob from MCP response.
2. Persist to disk under `~/.jenny/mcp-tool-output/` (unique filename with extension based on mime type).
3. Return human-readable path reference in tool_result.

Applies to `ReadMcpResource` (session-scoped) and MCP tool calls returning `image` or `blob` content parts.

## Content Truncation

Oversized MCP text responses truncate before model context:

- Default cap: **100,000 characters** (~25,000 tokens).
- Configurable via `MCP_MAX_OUTPUT_CHARS` environment variable.
- Truncation appends notice with original size.
- Truncated content still valid text.

## Pagination

`tools/list` and `resources/list` support cursor-based pagination per MCP spec:

- If response contains `nextCursor`, client sends follow-up request with `cursor` param.
- Continues until `nextCursor` is empty.
- All pages are aggregated transparently; callers get complete results.

## Resource Cache

Per-server LRU cache for `resources/list` and `resources/read`.

Invalidate cache on:

- Server disconnect
- Session expired
- `notifications/resources/list_changed`

## Progress Events (Not Implemented — P4)

Planned: During long MCP tool calls, emit progress entries (not chain nodes):

- `mcp_progress` with `status: started | completed`
- Yield separately from final tool_result in stream-json

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Server disconnect mid-call | Error tool_result; invalidate cache |
| Tool renamed on server | Refresh tool list on reconnect |
| Multiple content parts (text + blob) | Process each; blob to disk |
| Persist failure for binary | Error text; never inline base64 |
| Concurrent MCP calls same server | Serialize or use connection pool per server policy |

## Headless Protocol Compatibility

- `system`/`init` includes `mcp_servers: [{ name, status }]`.
- Tool progress may appear as `tool_progress` lines between assistant and user messages.
- Final tool_result uses text/path only, compatible with stream-json `user` message shape.

## Transport Interface

Both transports implement a common interface:

```go
type Transport interface {
    SendRequest(ctx context.Context, req jsonRPCRequest) (*jsonRPCResponse, error)
    SendNotification(ctx context.Context, notif jsonRPCRequest) error
    Close() error
}
```

This allows the Client to be transport-agnostic after connection. The `doRequest` method routes to the appropriate transport and handles session re-initialization for HTTP.

## Acceptance Criteria

- **AC1:** Stdio transport connects with valid `command` config (existing behavior).
- **AC2:** HTTP transport connects with valid `url` config, completes initialization handshake.
- **AC3:** HTTP transport handles both JSON and SSE response content types.
- **AC4:** HTTP transport includes `Mcp-Session-Id` in requests after initialization.
- **AC5:** HTTP transport includes custom `headers` from config in all requests.
- **AC6:** Client automatically re-initializes on HTTP 404 (session expired) and retries the failed request once.
- **AC7:** Binary MCP output persisted to disk; tool_result references path.
- **AC8:** Resource cache cleared on disconnect.
- **AC9:** Tools discovered via HTTP transport are registered identically to stdio tools.
- **AC10:** Existing stdio tests pass unchanged (no regression).
