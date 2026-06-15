---
title: EnterWorktree Tool
slug: enter-worktree
priority: P4
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - git-helpers
---
# EnterWorktree Tool

## Overview

Creates git worktree and switches session cwd to isolated copy.

## Rules

- Reject if already in worktree session
- Slug: alphanumeric segments, max 64 chars; random slug if omitted
- Resolve to canonical git root first
- Prompt/memory caches are **not** explicitly cleared; caches invalidate naturally via mtime/generation counters.

## Acceptance Criteria

- **AC1:** Reject double worktree entry.
- **AC2:** Slug validation enforced.
- **AC3:** Random slug when omitted.
- **AC4:** Caches invalidate naturally on cwd switch (no explicit flush).
