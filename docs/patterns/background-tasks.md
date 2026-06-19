---
title: Background Tasks
slug: background-tasks
priority: P4
status: done
spec: partial
code: done
package: internal/agent, internal/tool
gaps:
  - TaskManager API not fully documented
depends_on:
  - bash
  - tool-registry
---
# Background Tasks

## Overview

Long-running shell and agent tasks tracked with progress, output files, and parent notifications.

## Bash Background

- `run_in_background: true` spawns tracked shell task via TaskManager.
- Progress events emitted after ~2s.
- Output written to `.jenny/.../tasks/<id>.output` (append mode).
- Commands exceeding 120s in foreground receive a tip suggesting `run_in_background: true`.
- `sleep >=2` is disallowed in foreground mode; requires `run_in_background: true`.

## Agent Background

- `run_in_background: true` on the Agent tool launches async via `AsyncSubagentRunner.RunSubagentAsync`.
- Returns `status: async_launched` and `agent_id`.
- This is explicit; there is no automatic promotion of sync to async.

## TaskManager

The `TaskManager` (`internal/tool/task_manager.go`) manages background task lifecycle:

- State machine: `running` → `completed` / `stopped`
- SIGTERM-then-SIGKILL escalation (5s grace period) for task termination
- `FlushPartialOutput` for periodic output flushing
- `DrainCompletions` / `DrainCompletion` for the completion queue
- `EmitTaskProgress` for progress events

## TaskOutput

The `TaskOutput` tool (`internal/tool/task_output.go`) reads background task output:

- Parameters: `task_id`, `block` (blocking mode), `timeout` (default 30s, max 600s)
- Blocking mode: 100ms poll loop until output available or timeout
- Checks in-memory completion queue first, then falls back to output file

## Completion

Structured notification XML to parent agent on task completion. The engine injects `<task_completed task_id="..." duration_seconds="..." exit_code="..."/>` as a synthetic `tool_result` message.

## TaskStop

Only `running` tasks; accepts deprecated `shell_id` alias.

## Acceptance Criteria

- **AC1:** Background bash writes to output file.
- **AC2:** Progress after 2s.
- **AC3:** Completion notifies parent via structured XML.
- **AC4:** `sleep >=2` disallowed in foreground; requires `run_in_background: true`.
- **AC5:** TaskStop only for running tasks.
