---
title: Write Tool
slug: write
priority: P1
status: done
spec: partial
code: done
package: internal/tool
gaps: []
depends_on:
  - read
  - tool-registry
---
# Write Tool

## Overview

Write creates or overwrites files. Read-before-write contract enforced at constrained permission levels. At `PermissionUnrestricted`, all gates are skipped.

## Parameters

| Param | Description |
|-------|-------------|
| `file_path` | Target path |
| `content` | Full file content |

## Permission Level Gating

| Gate | Blocks at | Skipped at |
|------|-----------|------------|
| `WriteAllowed()` | read, analyze | edit+ |
| `PathConstrained()` | read, analyze, edit | unrestricted |
| `ReadBeforeWrite()` | read, analyze, edit | unrestricted |

## Read-Before-Write

1. ReadFileCache must contain path from prior Read in this session.
2. Reject if entry is partial view (`offset`/`limit` set on read).
3. Staleness: if `mtime > readTimestamp` → error (file changed since read).

## AllowedPaths

`SetAllowedPaths(paths)` restricts writes to specific path prefixes. Allowlisted paths bypass the cwd gate. Non-allowlisted paths fall back to scratchpad prefix check.

## Scratchpad Prefix

`$JENNY_SCRATCHPAD/` prefix is resolved to the session-scoped scratchpad directory. Escape via `..` is blocked.

## Write Behavior

- Create parent directories (`mkdir -p`).
- Atomic write: temp file in target directory → `Sync` → `os.Rename`. Cross-device (`EXDEV`) fallback via `copyAndReplace`. Windows: rename retry loop.
- Return structured patch diff in tool result.

## Post-Write Updates

- Update ReadFileCache with new content and mtime.
- Path-triggered skill activation when the skills framework is enabled.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Write without Read | Error code indicating read first |
| Partial read then Write | Reject partial view |
| Concurrent external edit | mtime staleness error |
| New file | Read may use empty content snapshot |

## Acceptance Criteria

- **AC1:** Write without ReadFileCache entry fails (at constrained permission levels).
- **AC2:** Stale mtime fails before write.
- **AC3:** Parent dirs created automatically.
- **AC4:** Result includes patch diff.
- **AC5:** ReadFileCache updated after success.
