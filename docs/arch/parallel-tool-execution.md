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

The agent loop uses `ToolExecutor` (`internal/agent/executor.go`) to run model-requested tools. Consecutive read-only tools execute in parallel; mutating and shell tools are serialized. Results are returned in **request order**, not completion order.

## ToolExecutor

```go
executor := NewToolExecutor(tools, cwd)
// or with cross-turn state (permission denial cache):
executor := NewToolExecutorWithStreamConfig(tools, cwd, streamCfg)
results, err := executor.Execute(ctx, toolUseBlocks)
```

## Partitioning (`partitionGroups`)

Tools are scanned in model request order and grouped:

| Tool class | Grouping | Execution |
|------------|----------|-----------|
| Read, Glob, Grep (and `ConcurrencySafe()` tools) | Consecutive batch | Parallel (semaphore-limited) |
| Bash | Consecutive batch | Serial within batch; sibling abort on failure |
| Write, Edit | Individual | Serial (one per group) |
| Unknown tool | Individual | Immediate synthetic error |

When tool class changes, the current batch is flushed and a new group starts.

## Concurrency Limit

Default max parallel tools: **10**. Override with `JENNY_MAX_TOOL_CONCURRENCY` env var.

## Bash Sibling Abort

When a **Bash** tool fails (execution error or `IsError` result):

- Cancel the batch context.
- Subsequent bash tools in the same batch receive `"Tool execution aborted due to sibling failure"`.
- Other tool types failing do **not** abort siblings.

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
| 10+ parallel reads | Cap at `JENNY_MAX_TOOL_CONCURRENCY` |
| Bash + Read same turn | Separate groups; reads may complete while bash runs serially |
| Interrupt mid-batch | Context cancellation marks pending tools interrupted |
| Duplicate tool names same turn | Each tool_use ID independent |

## Acceptance Criteria

- **AC1:** Read/Glob/Grep run in parallel when consecutive.
- **AC2:** Write/Edit/Bash never run concurrently with each other or with parallel batch.
- **AC3:** Bash failure aborts sibling bash in same batch.
- **AC4:** Unknown tool returns immediate error.
- **AC5:** Results emitted in request order.
