---
title: ListMcpResources Tool
slug: list-mcp-resources
priority: P2
status: done
spec: complete
code: done
package: internal/mcp
implemented:
  - "TTL-cached resource listing with generation invalidation"
  - "Bounded concurrent multi-server fetch"
  - "Partial failure resilience"
gaps: []
# Reviewed 2026-06-16: all gaps addressed
depends_on:
  - mcp-client
---
# ListMcpResources Tool

## Overview

Read-only listing of MCP resources from connected servers. Optional server filter.

## Parameters

| Param | Description |
|-------|-------------|
| `server` | Optional filter to one MCP server name |

## Behavior

- `server` set but no match → **error** listing available server names.
- Per connected server: fetch resources (TTL cache, 30s, with generation-based invalidation).
- Per-server failure → `[]` for that server (not whole-call failure).
- Disconnected clients skipped.

## Output

Empty aggregate: note that resources may be empty while tools still exist.

Non-empty: JSON array with `uri`, `name`, optional `mimeType`/`description`, `server`.

Partial failure: `{"resources":[...], "errors":{"server":"msg"}}`.

## Properties

- Concurrency-safe, read-only.
- Deferrable in tool search.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Cache stale | Invalidate on disconnect, resources/list_changed |
| Unknown server | Error with server list |
| Known server zero resources | Empty array + note |

## Acceptance Criteria

- **AC1:** No filter returns all connected servers' resources.
- **AC2:** Invalid server errors with available names.
- **AC3:** Partial failure returns partial results.
- **AC4:** Empty result includes tools-may-exist note.
- **AC5:** Each entry includes server field.
