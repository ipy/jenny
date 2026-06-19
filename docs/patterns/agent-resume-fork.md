---
title: Agent Resume and Fork
slug: agent-resume-fork
priority: P4
status: done
spec: partial
code: done
package: internal/agent, internal/tool
gaps:
  - Worktree state is persisted to transcript but not restored on resume
depends_on:
  - subagent-types
  - session-persistence
---
# Agent Resume and Fork

## Overview

Agent tool (wire name `agent`, lowercase) spawns subagents with sync/async execution, worktree isolation, and fork detection. The executor resolves `task` as an alias for `agent`.

## Fork Detection

- Recursive fork blocked via context value: `context.WithValue(ctx, tool.ForkChildKey, cfg.IsForkChild)`.
- Named agent nesting blocked via `tool.NamedAgentKey` context value (when swarm mode enabled).
- No transcript history inspection; detection is purely context-based.

## Worktree Isolation

`isolation: worktree` creates a temp worktree with a unique branch name.

Mutually exclusive with `cwd` override (returns error if both specified).

Worktree state (`WorktreePath`, `WorktreeBranch`, `WorktreeCWD`) is persisted to transcript but not yet restored on resume.

## Async Execution

- `run_in_background: true` launches subagent via `AsyncSubagentRunner`
- Returns status (`async_launched`) and `agent_id`
- Partial result on interrupt (context cancellation yields captured output)

## Acceptance Criteria

- **AC1:** Recursive fork blocked via context value.
- **AC2:** Worktree isolation mutually exclusive with cwd override.
- **AC3:** Async returns status and agent_id.
- **AC4:** Interrupt yields partial result.
