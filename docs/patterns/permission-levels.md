---
title: Permission Levels
slug: permission-levels
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/agent, internal/tool
gaps:
  - Implementation not started (see Migration Path)
depends_on:
  - dangerous-command-gate
  - bash
  - sandbox
---

# Permission Levels

## Overview

Replace the binary `skipPermissions bool` with a five-level `PermissionLevel` enum. Each level adds exactly one core capability over the previous, creating a predictable capability ladder. The model separates two orthogonal control axes — **Bash execution** and **file write** — that the current binary flag conflates.

### Problem Statement

The current `--dangerously-skip-permissions` flag is all-or-nothing:

1. **Over-permissive default**: Default mode allows file writes (Write/Edit) but restricts Bash to an 18-command read-only allowlist — yet the mode is not truly "read-only".
2. **No middle ground**: No way to grant Bash mutation (e.g., `mkdir`, `npm install`) without also allowing `rm -rf /`.
3. **Binary bypass**: `--dangerously-skip-permissions` removes ALL safety checks, including deterministic pattern blocks like command substitution and device paths.
4. **Headless mismatch**: Headless agents need graduated trust — pure analysis, read-only exploration, or full autonomy — but the binary switch cannot express these.

### Design Principles

- **Capability-additive**: Each level strictly extends the previous level's capabilities.
- **Deterministic**: All access decisions are pattern-based and auditable, never ML-classified.
- **Orthogonal axes**: Bash execution policy and file write policy are independent controls.
- **Backward-compatible**: `--dangerously-skip-permissions` maps to `unrestricted` level.

## Five Levels

| Level | Bash | File Write | Incremental Capability |
|-------|------|------------|----------------------|
| `read` | Blocked entirely | Blocked | Structured read-only tools only |
| `analyze` | Read-only allowlist (18 commands) | Blocked | + Bash read commands |
| `edit` | Read-only allowlist (18 commands) | cwd + scratchpad (read-before-write) | + File writes |
| `execute` | Pattern blocks enforced, pipeline allowlist skipped | cwd + scratchpad (read-before-write) | + Bash mutation commands (e.g. `mkdir`, `npm install`, `go test`) |
| `unrestricted` | No gate | No gate | + All safety boundaries removed |

### Level Details

#### `read` — Structured Read-Only

Agent can only use structured tools that read state: `Read`, `Grep`, `Glob`, `ListFiles`, `ReadMCPResource`, `WebFetch`, `WebSearch`. No Bash execution at all. No file modification through any tool. Note: `Read`/`Glob`/`Grep` are restricted to cwd + scratchpad paths (inherited from existing tool guards when `skipPermissions=false`).

Use case: Audit agents, documentation generators, CI lint reviewers that only need to analyze code without executing anything.

#### `analyze` — Read-Only Analysis via Bash

Extends `read` with Bash commands that are provably non-mutating. Every pipeline segment must pass `isSegmentReadOnly()`:

- Allowlist: `cat`, `head`, `tail`, `ls`, `find`, `grep`, `rg`, `git log`, `git diff`, `git show`, `git status`, `git branch`, `git remote`, `wc`, `sort`, `uniq`, `echo`, `true`
- Blocked: `$VAR` expansion (unquoted), output redirection (`>`, `>>`), pipes to mutating commands, `cd && git` escape patterns
- Write/Edit tools return permission denied

Use case: Security scanners, codebase analysts that need to run `git log --oneline | head -20` or `find . -name '*.go'` but must not modify anything.

#### `edit` — File Write Within Bounds

Extends `analyze` with file write capability via Write/Edit tools. Bash remains on the read-only allowlist.

- Write/Edit require `PathInWorkingDir()` — target must be within cwd or scratchpad
- Write/Edit require read-before-write — must `Read` the same path before modifying
- Bash still restricted to 18-command read-only allowlist
- Edit replaces file content in-place; Write creates or overwrites

This is the **current default behavior** (equivalent to running without `--dangerously-skip-permissions`).

Use case: Code modification agents that edit source files but should not run arbitrary shell commands like `npm install` or `go test`.

#### `execute` — Command Execution with Guardrails

Extends `edit` by flipping Bash pipeline validation from allowlist (default-deny) to **skipped** — pipeline segments are no longer restricted to the 18-command read-only allowlist. `CheckCommand()` pattern blocks remain enforced (substitution, injection, device paths), and `validateCommandPaths()` constrains referenced paths to cwd/scratchpad.

- **Allowed**: Any Bash command not matching blocklist patterns
- **Blocked**: Command substitution (`$()`, backticks), process substitution (`<()`, `>()`), device paths (`/dev/*`), proc environ (`/proc/*/environ`), git config injection (`git -c`, `--exec-path`), carriage return smuggling, ANSI-C quoting tricks
- `validateCommandPaths()`: Command-referenced paths must be within cwd or scratchpad — this is a filesystem boundary constraint (separate from the mutation-capability axis); it prevents path escape regardless of whether the command itself is mutating.
- Sandbox remains active: filesystem and network policy still enforced at OS level
- Write/Edit rules unchanged from `edit`

The pipeline strategy shift is the key security transition: `analyze`/`edit` enforce a read-only allowlist on every pipeline segment, while `execute` skips that allowlist — any command is allowed through the pipeline gate, but `CheckCommand()` still blocks syntactically dangerous patterns.

Use case: Full development agents that run `go test`, `npm install`, `mkdir`, `cp`, but must not escape sandbox or inject commands.

#### `unrestricted` — No Boundaries

All safety gates disabled. Equivalent to current `--dangerously-skip-permissions`.

- CommandGate: `CheckCommand()` and `CheckPipelineSegments()` skipped entirely
- Sandbox: Not wrapped
- Write/Edit: No `PathInWorkingDir()`, no read-before-write
- Intended ONLY for trusted, isolated environments

Use case: Emergency recovery, fully trusted CI pipelines, local development with explicit acknowledgment of risk.

## Bash Strategy Matrix

| Check | read | analyze | edit | execute | unrestricted |
|-------|------|---------|------|---------|-------------|
| Bash execution | Blocked | Allowed | Allowed | Allowed | Allowed |
| `CheckPipelineSegments()` | N/A | Enforced (allowlist) | Enforced (allowlist) | Skipped | Skipped |
| `CheckCommand()` pattern blocks | N/A | Enforced | Enforced | Enforced | Skipped |
| `validateCommandPaths()` | N/A | Enforced | Enforced | Enforced | Skipped |
| Sandbox wrap | N/A | Active | Active | Active | Skipped |
| Pipeline strategy | — | Allowlist | Allowlist | Skip read-only check | None |

## File Write Strategy Matrix

| Check | read | analyze | edit | execute | unrestricted |
|-------|------|---------|------|---------|-------------|
| Write/Edit tools | Blocked | Blocked | Allowed | Allowed | Allowed |
| `PathInWorkingDir()` | N/A | N/A | Enforced | Enforced | Skipped |
| Read-before-write | N/A | N/A | Enforced | Enforced | Skipped |

## Blocked Patterns at `execute` Level

At `execute`, `CheckCommand()` pattern blocks remain enforced — command substitution, process substitution, device paths, proc environ, git config injection, and all other dangerous constructs. These are the same patterns listed as "Blocked Patterns (All Modes)" in [dangerous-command-gate.md](../tools/dangerous-command-gate.md); see that doc for the full table.

## Sandbox Coordination

PermissionLevel and Sandbox are complementary layers:

| Layer | Scope | Enforced At |
|-------|-------|------------|
| PermissionLevel | Logical capability boundaries | Application (Go code) |
| Sandbox | OS-level filesystem/network | Kernel / sandbox-exec |

**Check ownership**: `CheckCommand()` and `CheckPipelineSegments()` belong to `CommandGate` (internal/tool/gate.go). `validateCommandPaths()` and path-based restrictions belong to `BashTool` (internal/tool/bash.go) — they are tool-level checks, not gate-level. Flipping to `unrestricted` skips both gate and tool-level checks.

At `execute` level, CommandGate blocks syntactic attacks (substitution, injection) while Sandbox constrains runtime effects (filesystem writes, network access). Neither is sufficient alone — together they provide defense in depth.

Sandbox `refreshConfig()` must be called when PermissionLevel changes at runtime (e.g., via session resume with a different level). See [sandbox.md](./sandbox.md) for `refreshConfig()` definition.

## CLI Changes

### New Flag

```
--permission-level <level>
```

Values: `read`, `analyze`, `edit`, `execute`, `unrestricted`

Default: `edit` (matches current behavior without `--dangerously-skip-permissions`)

### Configuration Precedence

Following [koanf-config.md](../arch/koanf-config.md) layering (highest to lowest):

1. **CLI Flag**: `--permission-level <level>`
2. **Environment Variable**: `JENNY_PERMISSION_LEVEL` → koanf key `permission-level`
3. **JSON Config**: `.jenny/config.json` key `"permission-level"` (dashed, matching koanf convention)

### Backward Compatibility

| Legacy Flag | Maps To |
|------------|---------|
| (no flag) | `--permission-level edit` |
| `--dangerously-skip-permissions` | `--permission-level unrestricted` |

When both `--dangerously-skip-permissions` and `--permission-level` are specified, `--dangerously-skip-permissions` takes precedence (unrestricted) and a warning is logged.

Verified: existing `skipPermissions=true` callers in `gate.go` and `bash.go` skip all checks; mapping to `unrestricted` preserves this behavior exactly.

## Migration Path

1. **Phase 1**: Add `PermissionLevel` enum; internal code reads it; CLI accepts `--permission-level`; default maps to `edit`; `--dangerously-skip-permissions` maps to `unrestricted`. No behavior change.
2. **Phase 2**: Wire `PermissionLevel` into `CommandGate` — `analyze`/`edit` enforce pipeline allowlist, `execute` skips it (pattern blocks remain). Wire into Write/Edit tool guards — `read`/`analyze` block writes.
3. **Phase 3**: Deprecation warning on `--dangerously-skip-permissions` (recommend `--permission-level unrestricted`).
4. **Phase 4**: Remove `--dangerously-skip-permissions` after deprecation period.

## Edge Cases

| Case | Expected Behavior |
|------|-------------------|
| `read` level + MCP tool that writes | MCP tool gating is out of scope for v1 (see below) |
| `analyze` level + `echo "hello" > file` | Blocked: output redirection fails read-only check |
| `edit` level + `npm install` via Bash | Blocked: `npm` not in 18-command allowlist |
| `execute` level + `rm -rf /` | Blocked by `validateCommandPaths()` (path outside cwd) + Sandbox filesystem policy |
| `execute` level + `$(whoami)` | Blocked by `CheckCommand()` pattern block (command substitution) |
| `unrestricted` level + `rm -rf /` | Allowed (no gate); operator assumes full risk |
| Resume session at different level | Apply new level; call `refreshConfig()` on Sandbox |
| Subagent inherits level | Subagent receives parent's PermissionLevel via its tool-context config struct; cannot escalate. See [swarm.md](./swarm.md). |

## Out of Scope (v1)

- **MCP tool capability classification**: The current model defines two axes (Bash execution, file write). MCP tools may mutate external state (APIs, databases) that falls outside both axes. A third axis (external mutation / network-write) or per-tool capability metadata is deferred to a future version. In v1, MCP tools execute regardless of PermissionLevel; operators should restrict MCP server availability via `--strict-mcp-config` or `--mcp-config` when running at `read`/`analyze` levels.

## Operational Notes

- **MCP at restricted levels**: When running at `read` or `analyze`, pair with `--strict-mcp-config` or explicit `--mcp-config` to prevent MCP tools from exceeding the level's intent.

## Acceptance Criteria

- **AC1:** `--permission-level read` blocks all Bash execution and all file writes.
- **AC2:** `--permission-level analyze` allows only read-only allowlisted Bash commands; Write/Edit return permission denied.
- **AC3:** `--permission-level edit` matches current default behavior (read-only Bash + file writes within cwd).
- **AC4:** `--permission-level execute` allows Bash mutation commands while blocking dangerous patterns (substitution, injection, device paths).
- **AC5:** `--dangerously-skip-permissions` maps to `unrestricted` with no behavior change.
- **AC6:** Both `--dangerously-skip-permissions` and `--permission-level` specified → unrestricted + warning logged to stderr (structured log, following [structured-logging.md](../arch/structured-logging.md)).
- **AC7:** Subagent receives parent's `PermissionLevel` via tool-context config struct and cannot escalate beyond it (see [swarm.md](./swarm.md)).
- **AC8:** Configuration precedence: CLI flag > `JENNY_PERMISSION_LEVEL` env > `.jenny/config.json` key (see [koanf-config.md](../arch/koanf-config.md)).
- **AC9:** Write/Edit at `edit` and `execute` require a prior `Read` of the same path; `unrestricted` skips the check.

## Related

- Dangerous command gate: [dangerous-command-gate.md](../tools/dangerous-command-gate.md)
- Bash tool: [bash.md](../tools/bash.md)
- Write tool: [write.md](../tools/write.md)
- Edit tool: [edit.md](../tools/edit.md)
- Sandbox abstraction: [sandbox.md](./sandbox.md)
- CLI flags: [cli.md](../arch/cli.md)
- Swarm (subagent level inheritance): [swarm.md](./swarm.md)
- Koanf config layering: [koanf-config.md](../arch/koanf-config.md)
