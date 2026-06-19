---
title: Read Tool
slug: read
priority: P1
status: done
spec: partial
code: done
package: internal/tool
implemented:
  - "Text file reading with line numbers"
  - "Offset/limit partial reads"
  - "Size limits (256KB pre-read, 25K tokens post-read, 1GiB hard limit)"
  - "Block device rejection"
  - "Path security (traversal prevention)"
  - "Read deduplication cache (mtime-based, TOCTOU-safe)"
  - "Image files (png, jpg, gif, webp) returned as base64 data URIs"
  - "Skill directory discovery on read paths"
  - "Scratchpad prefix resolution"
  - "UTF-16 LE/BE with BOM detection and decoding"
gaps:
  - "PDF reading (page extraction, poppler fallback) not implemented"
  - "Notebook (.ipynb) structured cell parsing not implemented"
  - "macOS screenshot filename retry (U+202F) not implemented"
defer_to: P3
depends_on:
  - tool-registry
---
# Read Tool

## Overview

Read returns file contents with line numbers, or structured blocks for images/PDFs/notebooks. Enforces size limits, read deduplication, and path security.

## Parameters

| Param | Description |
|-------|-------------|
| `file_path` | Absolute or relative path; resolved relative to cwd |
| `offset` | 1-based start line; `0` treated as line 1 |
| `limit` | Max lines to return |
| `max_size` | Override max file size in bytes |
| `max_tokens` | Override max token count |

## Size Limits

| Limit | Default | When checked | On exceed |
|-------|---------|--------------|-----------|
| `maxSizeBytes` | 256 KB | stat before read (full reads only) | Throw pre-read |
| `maxSizeHardLimit` | 1 GiB | stat before read | Throw pre-read |
| `maxTokens` | 25,000 | after read | Throw post-read (not silent truncate) |

Partial reads (`offset`/`limit`): file is read in full, then offset/limit slice the lines in memory. Partial reads skip the `maxSizeBytes` pre-read check.

## Binary Files

Image-extension allowlist triggers base64 encoding for png/jpg/gif/webp. All other files are read as text (no binary-file rejection blocklist).

## Images

Supported formats: PNG, JPEG, GIF, WebP (detected by extension).

- Files up to 10 MB are read and returned as `data:<mime>;base64,<encoded>` data URIs.
- Files exceeding 10 MB are rejected with an error.
- The model can process these as vision inputs.

## PDFs (Not Implemented — P3)

Planned:
- Small + model supports: inline document block.
- Large: extract pages to JPEGs; `pages` limits pages per request.
- Poppler fallback when native extraction fails.

## Notebooks (`.ipynb`) (Not Implemented — P3)

Planned: Parse cells as structured content. When oversized, suggest Bash/`jq` approach in error.

## Dedup (cache hit)

Same path + offset + limit + mtime unchanged since last read → return structured cache indicator:

```
[file unchanged since last read — cached content is current]
```

The response includes `CacheHit: true` on the ToolResult to allow programmatic detection.

**Not applied:** after Write/Edit cache entries, partial views, or when offset/limit differ.

## Block Devices

Reject without reading: `/dev/zero`, `/dev/urandom`, stdio fds, `/proc/self/fd/{0,1,2}`.

## Scratchpad Prefix

`$JENNY_SCRATCHPAD/` prefix in file paths is expanded to the session scratchpad directory. `..` traversal out of the scratchpad is blocked.

## Path Security

- Resolve relative paths to cwd via `filepath.Join`.
- Block device paths rejected (see Block Devices).
- Windows: `WindowsCommandGate` path validation applied.

## Side Effects

- Skill directory discovery on read paths.
- File-read listeners (LSP, history).
- Auto-memory freshness prefix when reading memory files (see memdir.md).

## Output Format

Line-numbered text: `%6s\t%s` (6-char right-padded line number, tab, content).

Empty file or offset past EOF: warning in result, not hard error.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Symlink outside cwd | Reject per path policy |
| UTF-16 LE/BE with BOM | Detect and decode to UTF-8 |
| File changes during read | mtime check on subsequent Write/Edit |
| 256KB file, limit 10 lines | Partial reads skip pre-read size check |

## Acceptance Criteria

- **AC1:** Files > 256KB rejected before read.
- **AC2:** Output > 25K tokens rejected after read.
- **AC3:** offset=0 reads from line 1.
- **AC4:** Unchanged file returns structured cache indicator with `CacheHit: true`.
- **AC5:** Block device paths rejected without read.
