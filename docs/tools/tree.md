---
title: Tree Tool
slug: tree
priority: P2
status: done
spec: complete
code: done
package: internal/tool
gaps:
  - "Subdirectory omission hint (first implementation is basic output + pagination)"
depends_on:
  - tool-registry
---
# Tree Tool

## Overview

Tree recursively lists directory contents as a formatted tree. It replaces `bash ls -R` with structured, token-efficient output using standard tree prefixes (`├──` / `└──` / `│   `).

## Parameters

| Param | Description |
|-------|-------------|
| `path` | Directory path (optional, default cwd) |
| `max_depth` | Max recursion depth (optional, default 2) |
| `show_hidden` | Show hidden files/dotfiles (optional, default false) |
| `cursor` | Pagination cursor from previous call (optional) |
| `limit` | Max entries per page (optional, default 100) |

## Output Format

```
/basename
├── folder/         [N entries]
│   └── file.go     [1234 bytes, 2024-01-15]
└── root.go         [567 bytes, 2024-01-14]
[... truncated]
```

- **Prefix**: `├──` (not last), `└──` (last), `│   ` (continuation)
- **Directory hint**: `[N entries]` / `[1 entry]` / `[empty]`
- **File hint**: `[N bytes, YYYY-MM-DD]`
- **Pagination**: `cursor` token allows resuming from last seen entry
- **Truncation**: `Truncated: true` when `limit` reached mid-directory

## Memory Protection

`os.ReadDir` is called in batches of `ReadDirBatchSize = 128` entries to prevent OOM on large directories. Large directories that exceed `limit` entries are truncated with a cursor.

## Path Security

- Relative paths resolved against cwd.
- Path traversal outside cwd is blocked when `PathConstrained()`.
- Non-directory paths return an error.

## Backend

Pure Go BFS traversal using `os.ReadDir` + `os.Stat`. No external command dependency.
