---
title: Tool Registry and Presets
slug: tool-registry
priority: P1
status: done
spec: complete
code: done
package: internal/tool
gaps: []
depends_on:
  - agent-loop
---
# Tool Registry and Presets

## Overview

Jenny builds the tool list via a **fluent builder API**. The registry assembles built-in tools first, then MCP tools, filtered by deny rules and feature flags.

## Registration Flow

```go
tools := tool.NewRegistry().
    WithBaseTools().
    WithReadFileCache(readFileCache).
    WithDenyRules(deniedTools).
    WithPermissionLevel(permLevel).
    WithStrictMCP(strictMCP).
    WithMCPTools(mcpTools).
    WithWebFetchEnabled(true).
    WithWebSearchEnabled(true).
    WithModel(model).
    WithSkillsFrameworkEnabled(!bare, skills).
    Build()
```

`WithBaseTools()` registers Read, Bash (or PowerShell on Windows), Glob, Grep, ReadMcpResource, McpPrompt, and — when `WithReadFileCache` is set — Write, Edit, NotebookEdit. Optional tools are added by their respective `With*` methods.

BashTool and PowerShellTool are always registered regardless of shell availability. The shell-presence check is internal to each tool. A shared ExecTool instance is created per registry and passed to both tools.

## Build Order

1. Instantiate base tools (platform-aware shell tool selection).
2. Wire sandbox, skill activator, task manager as configured.
3. Append optional tools (WebFetch, WebSearch, LSP, activate_skill, worktree, task tracking, …).
4. Filter by `WithDenyRules` and per-tool `WithEnabled`.
5. Append MCP tools; **built-in wins** on name collision.

Built-ins appear first for prompt cache stability.

## Deny Rules

`WithDenyRules([]string)` excludes tools by exact name. Denied tools never appear in the API tool list.

## Task tracking

Task tracking tools (TaskCreate, TaskGet, TaskUpdate, TaskList) are **off by default** and gated by the `enable-tasks` setting. The setting is available as:

- CLI flag: `--enable-tasks`
- Environment variable: `JENNY_ENABLE_TASKS`
- Settings JSON key: `enable-tasks`

When disabled (default), no TaskStore is created and `GetTaskStore()` returns nil. Session persistence of task state is skipped when the store is nil.

TaskStop and TaskOutput are independent and controlled by their own feature flags (`WithTaskStopEnabled`, `WithTaskOutputEnabled`).

## Feature Flags

| Tool / group | Builder method |
|--------------|----------------|
| Write / Edit / NotebookEdit | `WithReadFileCache(cache)` — gates registration |
| LSP | `WithLSPEnabled(true)` + `WithLSPClient(client)` |
| EnterWorktree / ExitWorktree | `WithEnterWorktreeEnabled` / `WithExitWorktreeEnabled` |
| WebFetch / WebSearch | `WithWebFetchEnabled` / `WithWebSearchEnabled` |
| TaskCreate / TaskGet / TaskUpdate / TaskList | `WithTaskTrackingEnabled(true)` |
| TaskStop | `WithTaskStopEnabled(true)` |
| TaskOutput | `WithTaskOutputEnabled(true)` |
| Skills framework | `WithSkillsFrameworkEnabled(true, skills)` |
| Sandbox | `WithSandbox(sandbox)` — wires into Bash and Grep |
| Strict MCP | `WithStrictMCP(true)` — suppresses all built-ins, MCP only |

## MCP Tools

Two modes are available for MCP tool integration:

**Static mode (default):** Individual MCP tools are merged after built-ins via `WithMCPTools`; names prefixed `mcp__<server>__<tool>` (convention applied by the MCP integration layer before tools reach the registry).

**Dynamic mode** (`--dynamic-mcp`): Individual MCP tools are not registered. A single `call_mcp` tool is registered instead, which accepts `server`, `tool`, and `arguments` parameters and delegates to the connected MCP client at call time. This keeps the tool list stable across session resume/continue even when MCP servers change.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Duplicate tool name | Dedupe; built-in wins over MCP |
| Tool disabled via deny rule | Removed at Build time |
| Structured output mode | Inject synthetic StructuredOutput tool (agent layer) |

## Acceptance Criteria

- **AC1:** Denied tools absent from model tool list.
- **AC2:** Task tracking tools registered only when `enable-tasks` is set; absent by default.
- **AC3:** LSP only when enabled with client.
- **AC4:** MCP tools appended with correct prefix.
- **AC5:** Tool list in system prompt matches registered set.
- **AC6:** When `WithStrictMCP(true)`, all built-in tools are suppressed.
