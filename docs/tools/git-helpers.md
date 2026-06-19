---
title: Git Helpers
slug: git-helpers
priority: P1
status: done
spec: partial
code: done
package: internal/git
gaps:
  - IsIgnored (gitignore matching) undocumented
  - CreateWorktree/RemoveWorktree undocumented
depends_on:
  []
---
# Git Helpers

## Overview

Filesystem-based git introspection for hot paths: repo root, worktrees, ref safety, shallow detection, cached branch/HEAD/remote. Worktree creation/removal spawns `git`.

## GetRoot(startPath)

Walk up from startPath seeking `.git` as directory **or** file (worktree/submodule).

- Memoized LRU cache (max 50 entries).
- Returns normalized path or error if not in a repo.

## .git File vs Directory

| Type | Resolution |
|------|------------|
| Directory | Regular repo; git dir = `{root}/.git` |
| File | Parse `gitdir: <path>`; resolve relative to repo root |

`resolveGitDir(startPath)`: memoized per cwd.

## Worktree commondir Validation

Security checks for malicious `.git` / `commondir`:

1. `worktreeGitDir` parent must be `{commonDir}/worktrees`
2. `{worktreeGitDir}/gitdir` realpath must equal `{realpath(gitRoot)}/.git`
3. Reject otherwise; fall back to input root

## Shallow Clone

`isShallowClone()`: true iff `{gitDir}/shallow` exists (per-worktree git dir, not shared commondir).

## Branch / HEAD / Remote Cache

Per-call `stat` check on HEAD, branch ref, and config files invalidates cached values.

Exported API:

- `GetBranch(rootPath)` — current branch or detached HEAD SHA
- `GetHead(rootPath)` — current HEAD SHA
- `GetRemoteUrl(rootPath)` — `remote.origin.url` from config

Worktree: branch refs + config read from the worktree's own `gitDir`.

## Ref Safety

`ValidateRefName(ref)` rejects leading `-`, shell metacharacters, `..`, `.lock` suffix, whitespace in refs/SHAs.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Detached HEAD | Raw hex SHA only |
| Symref chains | Follow loose refs only; **packed-refs not supported** (works for 99% of repos) |
| Submodule .git file | Separate repo if no commondir |
| Bare repo worktree | Canonical root may be common dir |

## Acceptance Criteria

- **AC1:** GetRoot cached and consistent for nested paths.
- **AC2:** Valid worktree resolves refs from worktree git dir.
- **AC3:** Malicious commondir rejected.
- **AC4:** isShallowClone detects shallow file in per-worktree gitDir.
- **AC5:** Cache invalidates on ref file mtime change.
