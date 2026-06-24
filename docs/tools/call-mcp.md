---
title: Call MCP Tool
slug: call-mcp
priority: P2
status: done
spec: complete
code: done
package: internal/tool
gaps: []
depends_on:
  - tool-registry
  - mcp-client
---
# Call MCP Tool

## Overview

The `call_mcp` tool provides dynamic MCP tool invocation without registering individual MCP tools in the model's tool list. It is registered when `--dynamic-mcp` is enabled, replacing the per-tool `mcp__<server>__<tool>` registration.

## Parameters

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `server` | string | yes | MCP server name (as configured) |
| `tool` | string | yes | Tool name on the MCP server |
| `arguments` | object | no | Tool input arguments (passed through to MCP) |

## Behavior

1. Normalize the `server` name and look up the connected MCP client.
2. If the server is not found, return an error listing available servers.
3. Call the tool on the MCP server with the provided arguments.
4. Return the MCP tool result (with content truncation and binary handling per `mcp-client.md`).

## Error Handling

| Condition | Result |
|-----------|--------|
| Missing `server` parameter | Error with message and list of available servers |
| Missing `tool` parameter | Error with message |
| Unknown server | Error listing available server names |
| MCP call failure | Error with MCP error details |

## Acceptance Criteria

- **AC1:** `call_mcp` registered only when `dynamic-mcp` is enabled.
- **AC2:** Individual MCP tools (`mcp__*`) not registered when `dynamic-mcp` is enabled.
- **AC3:** Successful tool calls return the MCP result.
- **AC4:** Unknown server returns error with available server names.
- **AC5:** Missing parameters return descriptive errors.
