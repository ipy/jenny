---
title: Swarm (Parallel Agents)
slug: swarm
priority: P4
status: done
spec: partial
code: done
package: internal/agent, internal/tool
gaps:
  - Worktree isolation, async execution, cost merge, profile routing, skill-specific tool filtering not documented here (see subagent-types.md)
depends_on:
  - subagent-types
---
# Swarm (Parallel Agents)

## Overview

Flat roster of parallel agents — no nested subagents. Team coordination gated behind `--swarm` CLI flag / `JENNY_SWARM` env var.

## Rules

- Flat roster only — no subagent (named or unnamed) may spawn another subagent (`isForkChild` check blocks all nesting). Named agents are additionally blocked from nested named-agent spawning (`isNamedAgent` check).
- `swarmsEnabled` field on `AgentTool` (set via `--swarm` flag) gates team tools together.
- Teammate spawn via Agent tool `name` param.
- Jenny runs all subagents in-process via `LocalSubagentRunner`.
- **Permission level inheritance**: Subagent receives parent's `PermissionLevel` via StreamConfig; cannot escalate. See [permission-levels.md](./permission-levels.md).
- **Named agent config inheritance**: Named agents inherit parent's full tool registry and 14+ StreamConfig fields (MCPConfig, memory, skills, budget, system prompt, structured schema, permission level).

## Out of Scope (Headless v1)

SendMessage, team delete, coordinator messaging.

## Acceptance Criteria

- **AC1:** No nested subagents (neither named nor unnamed).
- **AC2:** Swarm feature flag (`--swarm`) gates all team tools.
- **AC3:** Flat delegation only in headless mode.
- **AC4:** Subagent inherits parent's PermissionLevel and cannot escalate (see [permission-levels.md](./permission-levels.md) AC7).
