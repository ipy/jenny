---
title: Grep Tool
slug: grep
priority: P1
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# Grep Tool

## Overview

Grep searches file contents via ripgrep. Read-only, concurrency-safe.

## Parameters

| Param | Default | Description |
|-------|---------|-------------|
| `pattern` | required | Regex pattern |
| `path` | cwd | Search path |
| `glob` | — | File filter |
| `output_mode` | `files_with_matches` | `content`, `files_with_matches`, `count` |
| `head_limit` | 250 | Max matches; `0` = unlimited |
| `offset` | 0 | Skip first N matches (pagination) |
| `-i`, `-n`, `-A`, `-B`, `-C` | — | Ripgrep passthrough |
| `multiline` | false | Multiline mode |
| `type` | — | File type filter |

## Ripgrep Invocation

- `--hidden` enabled.
- Auto-exclude VCS dirs (`.git`, `.svn`, etc.).
- `--max-columns 500` to avoid base64 line bloat.
- Pattern starting with `-`: use `-e` flag.

## Backend Selection

The GrepTool prefers ripgrep (`rg`) when it is available on the host
and falls back to a small in-process Go search engine otherwise. The
selection is automatic, per-call:

1. **Sandbox active with a configured `RipgrepConfig.Command`** —
   use the sandboxed binary. If that binary is missing, fall through
   to step 2.
2. **`rg` is on `PATH`** — shell out to it.
3. **Otherwise** — call the in-process backend in
   `internal/grepinproc`. This backend uses `filepath.WalkDir` and
   `regexp.FindAllIndex` and produces the same text format as
   ripgrep, so the post-processing pipeline (head_limit, offset, 20K
   char cap) works identically for both backends.

The fallback exists so jenny can run in environments where ripgrep
cannot be installed (locked-down CI, minimal containers, no
package manager). The trade-off vs. ripgrep:

| | ripgrep | in-process |
|---|---|---|
| Speed on large repos | very fast (Rust + SIMD) | slower (pure Go) |
| External binary required | yes | no |
| `.gitignore` integration | yes (rg-native) | no (uses `.git`/`.svn` skip only) |
| `--type` | yes (rg built-in types) | yes (subset, see `internal/grepinproc/adapter.go`) |
| Symlink handling | rg-native | follows `os.Stat` |

Both backends honor the same input schema and produce the same
output format. Callers (and tests) do not need to know which
backend ran.

## Output Limits

- Total output capped ~**20K characters** (`maxResultSizeChars`).
- Ripgrep **timeout → error** (not empty result).
- Default mode `files_with_matches`; sort by mtime.

## Pagination

`offset` + `head_limit` apply across all output modes.

## Sandbox

When sandbox enabled, use configured sandboxed ripgrep path (see sandbox.md).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| No matches | Empty result with clear message |
| Binary files | Skipped by ripgrep |
| Invalid regex | Error from ripgrep surfaced |
| Huge match count | head_limit + output cap |

## Acceptance Criteria

- **AC1:** Default head_limit 250; 0 unlimited.
- **AC2:** Pattern `-foo` uses `-e`.
- **AC3:** Timeout returns error.
- **AC4:** Output capped at ~20K chars.
- **AC5:** VCS dirs excluded.
