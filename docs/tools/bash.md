---
title: Bash Tool
slug: bash
priority: P1
status: done
spec: complete
code: done
package: internal/tool
gaps: []
defer_to: P3
depends_on:
  - tool-registry
  - dangerous-command-gate
---
# Bash Tool

## Overview

Bash executes shell commands with permission classifier, optional sandbox, read-only constraints, output limits, and background execution support.

## Parameters

| Param | Description |
|-------|-------------|
| `command` | Shell command string |
| `timeout` | Max execution time in **seconds** (default 30) |
| `run_in_background` | Spawn tracked background task |
| `dangerouslyDisableSandbox` | Per-invocation sandbox opt-out (internal) |

## Permission Flow

Bash execution passes through layered security checks:

1. **Blocked pattern check** — reject command substitution, device paths, git injection, and other dangerous constructs
2. **Device path check** — reject references to device files
3. **Pipeline segment check** — read-only allowlist enforced per pipeline segment (at `analyze`/`edit` levels)
4. **Path boundary validation** — command-referenced paths must be within cwd/scratchpad (unless `cd` command)
5. **Sandbox wrapping** — command wrapped in OS-level sandbox (unless per-invocation opt-out)

`--dangerously-skip-permissions` (or `--permission-level unrestricted`) bypasses all gate checks.

## Read-Only Mode

- Massive allowlist with flag-level validation.
- Pipelines: every segment must pass read-only check.
- Concurrency safety flag is true only for read-only commands.

## Sandbox

Wrap command via sandbox backend when enabled (see sandbox.md).

## Sed Simulation

In-place `sed` edits may be simulated as file edits internally:

- Parse sed command → apply as Edit/Write.
- Sed simulation is never exposed in the tool schema.
- No git attribution; writes files directly via Edit/Write internals.

## Output Limits

- Inline cap ~**30K characters**.
- Larger output spilled to disk; tool result references path.

## Timeout and Cwd

- Default/max timeout from tool config.
- After execution: cwd is reset if it has drifted outside the project root.
- On context cancellation, the entire process group is terminated to catch grandchildren spawned by the shell.

## Background Execution

- `run_in_background`: spawn tracked shell task.
- Progress events after ~2s.
- Block standalone `sleep ≥2` seconds — use TaskOutput with block=true.
- Auto-background hint emitted for foreground commands exceeding **120s**.

## Exit Codes

Non-zero exit codes are returned as-is in tool output; the agent interprets them from stdout/stderr context.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Bash fails in parallel batch | Abort sibling bash processes |
| Sandbox unavailable | Fail with clear reason if sandbox required |
| Output spill disk full | Error with partial path if any |
| Heredoc / substitution | Blocked by dangerous-command gate |

## Acceptance Criteria

- **AC1:** Read-only pipelines validated per segment.
- **AC2:** Output >30K spilled to disk.
- **AC3:** sleep ≥2 blocked in foreground bash.
- **AC4:** Cwd reset when outside project.
- **AC5:** Sed simulation invisible in schema.
