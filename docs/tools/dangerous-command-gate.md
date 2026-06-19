---
title: Dangerous Command Gate
slug: dangerous-command-gate
priority: P1
status: done
spec: partial
code: done
package: internal/tool
gaps:
  - Heredoc injection detection not implemented
  - Unicode whitespace normalization not implemented
depends_on:
  - permission-levels
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
| ANSI-C quoting tricks | `$'...'` exploit tokenization differential |
| Brace expansion | `{}` with `,` or `..` (both balanced and unbalanced) |
| Carriage return smuggling | `\r` in tokens |
| Device paths | `/dev/null`, `/dev/zero`, `/dev/urandom`, `/dev/random`, `/dev/full`, `/dev/stdin`, `/dev/stdout`, `/dev/stderr`, `/dev/fd/*`, `/proc/self/fd/*` |
| Proc environ | `/proc/*/environ` (uses `strings.Contains` matching) |
| Git config injection | `git … -c`, `--exec-path`, `--config-env` |

## Read-Only Pipeline Validation

Every pipeline segment must pass read-only allowlist:

- `echo`, `true` and similar commands pass the read-only allowlist regardless of chain operator.
- Unquoted `$VAR` expansion fails read-only check (special shell variables `$?`, `$#`, etc. are permitted).
- `git` commands blocked because `git` is not in the read-only allowlist (no dedicated pattern).
- All segments must be read/search commands.

Prefer flag-level validation over regex where possible.

## Pattern-Based Gate

Security validation is **deterministic and auditable** via pattern checks — no ML classifiers. The command gate blocks dangerous constructs; pipeline segment validation enforces read-only allowlists on each segment.

## Bypass

Permission levels control which checks are enforced. See [permission-levels.md](../patterns/permission-levels.md) for the full model.

| Level | Blocked pattern check | Pipeline segment check | Pipeline strategy |
|-------|----------------------|----------------------|-------------------|
| `read` | N/A (Bash blocked) | N/A | — |
| `analyze` | Enforced | Enforced | Allowlist |
| `edit` | Enforced | Enforced | Allowlist |
| `execute` | Enforced | Skipped | Skip read-only check |
| `unrestricted` | Skipped | Skipped | None |

Legacy `--dangerously-skip-permissions` maps to `unrestricted` level — all security checks bypassed entirely. Must be explicit CLI flag; never default in headless production.

## CheckDevicePathsInCommand

`CheckDevicePathsInCommand()` scans all tokens in a command for device paths. Called from the Bash tool before execution, separate from single-path `CheckDevicePath`.

## Windows Command Gate

On Windows, `WindowsCommandGate` blocks commands like `Set-ExecutionPolicy`, `reg.exe`, `sc.exe` and paths like `C:\Windows\System32`, `AppData`, `\\.\PhysicalDrive`. Called from both `CheckCommand()` and `CheckDevicePath()`.

## Read Tool Device Blocks

Same device path blocklist for Read tool without reading content.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Nested substitution | Block even if inner looks safe |
| Read-only `git log` | Blocked: `git` not in read-only allowlist |

## Acceptance Criteria

- **AC1:** Command substitution blocked before execution.
- **AC2:** Pipeline security gate rejects mutating segment (output redirection, non-allowlisted commands).
- **AC3:** Git `-c` injection blocked by security gate.
- **AC4:** Bypass only via `--permission-level unrestricted` or legacy `--dangerously-skip-permissions` flag (see [permission-levels.md](../patterns/permission-levels.md) AC5/AC6).
- **AC5:** Device paths blocked in Read and Bash.
