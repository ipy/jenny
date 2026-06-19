---
title: Parallel Tool Execution
slug: parallel-tool-execution
priority: P1
status: done
spec: complete
code: done
package: internal/agent
gaps:
  []
depends_on:
  - agent-loop
  - tool-registry
---
# Parallel Tool Execution

## Overview

The agent loop uses a tool executor to run model-requested tools. Consecutive read-only tools execute in parallel; mutating and shell tools are serialized. Results are returned in **request order**, not completion order.

## ToolExecutor

The tool executor accepts a tool set and working directory (optionally with cross-turn state for permission denial caching) and returns results in request order.

## Partitioning (`partitionGroups`)

Tools are scanned in model request order and grouped:

| Tool class | Grouping | Execution |
|------------|----------|-----------|
| Read, Glob, Grep (and `ConcurrencySafe()` tools) | Consecutive batch | Parallel (semaphore-limited) |
| Bash, PowerShell (shell tools) | Consecutive batch | Serial within batch; sibling abort on failure |
| Write, Edit, NotebookEdit, TodoWrite, TaskCreate/Update/Stop, EnterWorktree, ExitWorktree | Individual | Serial (one per group) |
| Unknown tool | Individual | Immediate synthetic error |

When tool class changes, the current batch is flushed and a new group starts.

## Concurrency Limit

Default max parallel tools: **10**. Override with `JENNY_MAX_TOOL_CONCURRENCY` env var or `--max-tool-concurrency` CLI flag (see [koanf-config.md](./koanf-config.md) for precedence).

## Shell Sibling Abort

When a **Bash** or **PowerShell** tool fails (execution error or `IsError` result):

- Cancel the batch context.
- Subsequent shell tools in the same batch receive `"Tool execution aborted due to sibling failure"`.
- Other tool types failing do **not** abort siblings.

## MCP Progress Tokens

Every tool execution context is wrapped with an MCP progress token (`mcp.WithProgressToken(ctx, toolUseID)`). This allows MCP tools to report progress back to the client using the `tool_use_id` as the progress token.

## Unknown Tool

Immediate synthetic error — never hang:

```
Error: No such tool available: {name}
```

## Result Ordering

`Execute()` writes results into a slice indexed by original request position. Emission to stream-json follows this order.

## Permission Denial Cache

When `StreamConfig` is provided, security-gate denials are cached by `BuildDenialKey(toolName, input)`. Subsequent identical invocations return the cached denial without re-executing.

## Cwd Updates

Tools that return `NewCwd` update the executor's cwd. Updates from serial tools apply immediately; parallel batch cwd updates are mutex-protected.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| 10+ parallel reads | Cap at `JENNY_MAX_TOOL_CONCURRENCY` (or `--max-tool-concurrency`) |
| Bash + Read same turn | Separate groups; reads may complete while bash runs serially |
| Interrupt mid-batch | Context cancellation marks pending tools interrupted (sets `Interrupted` field on result) |
| Duplicate tool names same turn | Each tool_use ID independent |

## Acceptance Criteria

- **AC1:** Read/Glob/Grep run in parallel when consecutive.
- **AC2:** Write/Edit/Bash never run concurrently with each other or with parallel batch.
- **AC3:** Shell tool failure aborts sibling shell tools in same batch.
- **AC4:** Unknown tool returns immediate error.
- **AC5:** Results emitted in request order.
