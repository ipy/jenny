---
title: ReadMcpResource Tool
slug: read-mcp-resource
priority: P2
status: done
spec: partial
code: done
package: internal/tool
implemented:
  - "Read resource by server+URI"
  - "List resource templates"
  - "URI template expansion"
  - "Binary content persistence to disk"
gaps: []
depends_on:
  - mcp-client
---
# ReadMcpResource Tool

## Overview

Dual-action tool: reads a single MCP resource by server+URI, or lists available resource templates. Binary content persisted to disk — never inline base64.

## Parameters

| Param | Description |
|-------|-------------|
| `action` | `read` or `list_templates` (required) |
| `server` | MCP server name (required for `read`) |
| `uri` | Resource URI (required for `read` unless `template` provided) |
| `template` | URI template string; mutually exclusive with `uri` |
| `arguments` | Map for `{placeholder}` substitution in template |

## Validation

- Unknown server → error with available servers.
- Not connected → error.
- Missing resources capability → error.

## Execution

MCP `resources/read` with URI.

Per content item:

| Type | Handling |
|------|----------|
| text | Pass through in result |
| blob (base64) | Decode, persist to disk, return path in text |

Persist failure → error string, no inline blob.

## Output

`read` action: JSON `{ contents: [{ uri, mimeType?, text?, blobSavedTo? }] }`

`list_templates` action: plain text listing `"Available MCP Resource Templates:\n- <template> (server: <name>): <desc>\n"`

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Multiple content parts | Process each independently |
| Oversized text | Subject to global tool result truncation |
| Unique persist IDs | timestamp + random suffix |

## Acceptance Criteria

- **AC1:** Unknown server errors clearly.
- **AC2:** Text inline in text field.
- **AC3:** Binary on disk; path in result only.
- **AC4:** Persist failure not base64 inline.
- **AC5:** Concurrency-safe read-only.
