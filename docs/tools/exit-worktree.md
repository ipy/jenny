---
title: ExitWorktree Tool
slug: exit-worktree
priority: P4
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - enter-worktree
---
# ExitWorktree Tool

## Overview

Exits worktree session and optionally removes worktree.

## Parameters

| Param | Description |
|-------|-------------|
| `action` | `keep` or `remove` |
| `discard_changes` | Required true to remove dirty worktree |

## Behavior

- If worktree dirty-state cannot be determined, fail-closed (refuse removal)
- Restore original cwd via `NewCwd` result field
- `remove`: removes worktree directory and deletes the associated branch
- `keep`: leave worktree on disk, restore parent cwd

## Acceptance Criteria

- **AC1:** remove dirty worktree requires discard_changes.
- **AC2:** Unknown git state fails closed.
- **AC3:** Original cwd restored.
- **AC4:** keep action preserves worktree files.
