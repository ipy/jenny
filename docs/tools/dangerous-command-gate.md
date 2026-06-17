---
title: Dangerous Command Gate
slug: dangerous-command-gate
priority: P1
status: partial
spec: complete
code: partial
package: internal/tool
gaps:
  - CheckPipelineSegments() skip path at execute level
  - Pipeline strategy parameter on CommandGate (allowlist vs skip)
  - PermissionLevel → gate wiring
depends_on:
  - tool-registry
---
# Dangerous Command Gate

## Overview

Before Bash execution, commands pass security validation independent of sandbox. Read-only levels (`analyze`/`edit`) add pipeline-level checks. Bypass requires `--permission-level unrestricted` or legacy `--dangerously-skip-permissions`.

## Blocked Patterns (All Modes)

| Category | Examples |
|----------|----------|
| Command substitution | `$()`, `${}`, backticks |
| Process substitution | `<()`, `>()`, `=()` |
| Zsh extras | `=cmd`, `$[`, `~[` |
| ANSI-C / locale quoting tricks | Exploit tokenization differential |
| Brace expansion mismatch | Unbalanced `{}` |
| Carriage return smuggling | `\r` in tokens |
| Device paths | `/dev/zero`, `/dev/urandom`, `/dev/random`, `/dev/full`, stdio fds |
| Proc environ | `/proc/*/environ` |
| Git config injection | `git … -c`, `--exec-path`, `--config-env` |

## Read-Only Pipeline Validation

Every pipeline segment must pass read-only allowlist:

- Semantic-neutral commands (`echo`, `true`) skipped in `||` chains only.
- Unquoted `$VAR` / globs fail read-only check.
- `cd && git` escape patterns blocked.
- All segments must be read/search commands.

Prefer flag-level validation over regex where possible.

## Pattern-Based Gate

Security validation is **deterministic and auditable** via `CommandGate` pattern checks — no ML classifiers. `CheckCommand()` blocks dangerous constructs; `CheckPipelineSegments()` enforces read-only allowlists on pipeline segments.

## Bypass

Permission levels control which checks are enforced. See [permission-levels.md](../patterns/permission-levels.md) for the full model.

| Level | `CheckCommand()` | `CheckPipelineSegments()` | Pipeline strategy |
|-------|-----------------|--------------------------|-------------------|
| `read` | N/A (Bash blocked) | N/A | — |
| `analyze` | Enforced | Enforced | Allowlist |
| `edit` | Enforced | Enforced | Allowlist |
| `execute` | Enforced | Skipped | Skip read-only check |
| `unrestricted` | Skipped | Skipped | None |

Legacy `--dangerously-skip-permissions` maps to `unrestricted` level — skips classifier and security checks entirely. Must be explicit CLI flag; never default in headless production.

## Read Tool Device Blocks

Same device path blocklist for Read tool without reading content.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Nested substitution | Block even if inner looks safe |
| Unicode whitespace tricks | Normalize or reject |
| Heredoc injection | Block dangerous heredoc patterns |
| Read-only `git log` | Allow if no injection flags |

## Acceptance Criteria

- **AC1:** Command substitution blocked before execution.
- **AC2:** Pipeline security gate rejects mutating segment (output redirection, non-allowlisted commands).
- **AC3:** Git `-c` injection blocked by security gate.
- **AC4:** Bypass only via `--permission-level unrestricted` or legacy `--dangerously-skip-permissions` flag (see [permission-levels.md](../patterns/permission-levels.md) AC5/AC6).
- **AC5:** Device paths blocked in Read and Bash.
