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
- Concurrency safety is always disabled; all bash commands execute serially to maintain consistent cwd state.

## Sandbox

Wrap command via sandbox backend when enabled (see sandbox.md).

## Sed Simulation

In-place `sed` edits may be simulated as file edits internally:

- Parse sed command → apply as Edit/Write.
- Sed simulation is never exposed in the tool schema.
- No git attribution; writes files directly via `os.WriteFile`.

## Output Limits

- Inline cap ~**30K bytes**.
- Larger output spilled to disk; tool result references path.

## Timeout and Cwd

- Default timeout is 30 seconds; override with the `timeout` parameter.
- After execution: cwd is reset if it has drifted outside the project root.
- On context cancellation or timeout, the entire process tree is terminated to catch grandchildren spawned by the shell:
  - **Unix**: Uses process groups (`Setpgid`) and `SIGKILL` to the group leader.
  - **Windows**: Uses `taskkill /F /T /PID` to terminate the process tree.

## Environment

Every command (foreground and background) gets `JENNY_SCRATCHPAD` injected into its environment, pointing to the session scratchpad directory.

## Background Execution

- `run_in_background`: spawn tracked shell task.
- Progress events after ~2s.
- Block standalone `sleep ≥2` seconds — use TaskOutput with block=true.
- Auto-background hint emitted for foreground commands exceeding **120s**.
- Background task timeout uses two-phase kill: SIGTERM first, then SIGKILL after 5 seconds.

## Exit Codes

Non-zero exit codes are returned as-is in tool output; the agent interprets them from stdout/stderr context.

## ExecTool Architecture

BashTool uses `ExecTool` (`internal/tool/exec.go`) for all process execution. Both foreground and background paths go through `ExecTool.Start` + `handle.Wait`.

ExecTool owns: process group setup, env injection, kill escalation, and exit code mapping.

Kill escalation:

- **Unix:** SIGTERM → 5s wait → SIGKILL to the process group (cancellable goroutine).
- **Windows:** single `taskkill /F /T`.

## Simple-Command Fallback

When no shell is available (`resolveShell()` fails), BashTool falls back to `parseSimpleCommand`.

The parser tokenizes the command with a quote-aware state machine, checks for shell operators, builtins, and shell binaries. If the command is a simple argv (no operators, no always-reject builtins, executable found in PATH), it runs directly via ExecTool. If not parseable, it returns a structured error with `shell_unavailable` category.

Shell resolution tries, in priority order: bash, sh, dash, ash, zsh, ksh.

When the sandbox is active and no shell is available, the fallback refuses execution with `sandbox_violation` — it cannot honor string-rewriting sandbox in shell-less mode.

## Error Format

All tool errors use a structured format:

```
error: <category>
reason: <one-line cause>
input: <echoed input>
unsupported: <features list>
hint: <corrective action>
```

Categories: `permission_denied`, `unsupported_feature`, `shell_unavailable`, `path_out_of_scope`, `timeout`, `sandbox_violation`, `executable_not_found`, `execution_failed`.

## PowerShell

PowerShellTool follows the same ExecTool architecture and has its own simple-command fallback with PowerShell-specific operator tables. See `powershell.go`.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Bash fails in parallel batch | Abort sibling bash processes |
| Sandbox unavailable | Command executes without sandbox (no error if sandbox not configured) |
| Output spill disk full | Error with truncated inline output |
| Heredoc / substitution | Blocked by dangerous-command gate |
| No shell, simple argv | Runs via fallback through ExecTool |
| No shell, complex input | Structured `shell_unavailable` error |
| Sandbox active + no shell | `sandbox_violation` error |

## Acceptance Criteria

- **AC1:** Read-only pipelines validated per segment.
- **AC2:** Output >30K bytes spilled to disk.
- **AC3:** sleep ≥2 blocked in foreground bash.
- **AC4:** Cwd reset when outside project.
- **AC5:** Sed simulation invisible in schema.
