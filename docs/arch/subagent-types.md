---
title: Subagent Types
slug: subagent-types
priority: P4
status: done
spec: partial
code: done
package: internal/agent, internal/tool
gaps:
  - omitProjectInstructions field is dead code (set but never read)
  - mcpServers field is defined but all types have empty lists; RequiredMCPServers() never called outside tests
depends_on:
  - tool-registry
  - multi-provider-routing
  - session-persistence
---
# Subagent Types

## Overview

Built-in subagent types with distinct tool allowlists, models, and resume semantics.

## Built-in Types

| Type | allowedTools | deniedTools | oneShot | Notes |
|------|-------------|-------------|---------|-------|
| general-purpose | `*` (all) | — | false | Default subagent |
| explore | Read, Glob, Grep, Bash | Write, Edit, Agent | true | Read-only exploration |
| plan | Read, Glob, Grep | Write, Edit, Bash, Agent | true | Read-only planning |
| shell | Bash, Read, Glob, Grep | — | false | Command execution |
| verification | Read, TaskOutput, TaskStop | Write, Edit | false | Background task monitoring |

Each type has a `Description` string, optional `model` (`"inherit"` or alias), and `deniedTools` list.

## Filtering

`FilterTools(denied)` combines the type's `deniedTools` with caller-provided deny rules and returns allowed tools. Deny rules support wildcard suffix (e.g., `mcp__server__*`).

`AllowedTools` intersection: `SubagentParams.AllowedTools` (from skill activators) is intersected with the subagent type's filtered tool list, further restricting available tools.

## Named Agents (Swarm Mode)

When `--swarm` flag is enabled, the Agent tool accepts a `name` parameter for named agents:

- Named agents inherit the parent's full tool registry (no type-based filtering)
- Named agents inherit parent's `StreamConfig` fields: MCPConfig, memory, skills, budget, system prompt, structured schema, permission level
- Nested named agents are blocked via `NamedAgentKey` context value (AC1)
- When swarm mode is disabled, passing `name` returns an error

## Worktree Isolation

`SubagentParams.Isolation == "worktree"` creates a git worktree for the subagent:

- Mutually exclusive with `cwd` parameter
- Creates a unique branch (`worktree-<type>-<timestamp>`)
- Worktree state persisted to transcript via session manager
- Worktree cleaned up on exit via `defer git.RemoveWorktree()`

## Async Execution

`AsyncSubagentRunner` wraps `LocalSubagentRunner` for non-blocking execution:

- `RunSubagentAsync()` returns immediately with `AsyncResult` containing a `Done` channel
- `run_in_background` parameter on the Agent tool controls sync vs async
- Subagent result written to parent's transcript on completion

## Permission Inheritance

Named subagents inherit `PermissionLevel` from parent `StreamConfig` (AC7). Anti-escalation is enforced: the subagent's tool instances are shared from the parent, so permission level is enforced at the tool level.

## Router Profiles

`SubagentParams.Profile` switches the router's active profile for the duration of the subagent call (e.g., `"vision"`). The parent's profile is restored on return.

## Model Aliases

`sonnet`, `opus`, `haiku` map to concrete models when passed as the subagent `model` parameter. Unknown values pass through unchanged (direct pass-through; no separate resolver step at runtime).

## One-Shot Types

Explore, Plan: cannot resume (`oneShot: true` on the SubagentType struct; `CanResume()` returns `!oneShot`).

## Acceptance Criteria

- **AC1:** Each type has distinct tool allowlist; named agents blocked from spawning nested named agents.
- **AC2:** Deny rules filter subagent types; worktree isolation mutually exclusive with cwd.
- **AC3:** Model aliases resolve correctly; named agents inherit parent StreamConfig fields.
- **AC4:** One-shot types reject resume; AllowedTools intersection restricts skill-specific tools.
- **AC5:** ~~MCP requirements enforced per type.~~ (Not implemented: all types have empty mcpServers; RequiredMCPServers() unused.)
- **AC6:** Async execution via `run_in_background` returns `Done` channel.
- **AC7:** PermissionLevel inherited from parent; anti-escalation enforced.
