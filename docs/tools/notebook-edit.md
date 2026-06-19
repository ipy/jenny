---
title: Notebook Edit Tool
slug: notebook-edit
priority: P2
status: done
spec: partial
code: done
package: internal/tool
gaps: []
depends_on:
  - read
  - tool-registry
---
# Notebook Edit Tool

## Overview

Modifies Jupyter `.ipynb` files only. Modes: replace, insert, delete. Read-before-write via ReadFileCache. Not concurrency-safe — must execute serially.

## Parameters

| Param | Required | Description |
|-------|----------|-------------|
| `notebook_path` | yes | Absolute or relative `.ipynb` |
| `edit_mode` | no | `replace` (default), `insert`, `delete` |
| `cell_id` | conditional | Required for replace/delete; optional for insert-at-beginning |
| `cell_type` | insert only | `code` or `markdown` |
| `new_source` | replace/insert | Cell source string |

## Validation

- Extension must be `.ipynb` else error → use file Edit tool.
- ReadFileCache must contain path (Read first).
- mtime > readTimestamp → stale error.
- Invalid JSON → error.
- Missing cell → error; supports `cell-N` numeric index alias.

## Execution

| Mode | Behavior |
|------|----------|
| replace | Set source; reset execution_count/outputs for code cells. Fails with error if cell not found. Returns unified diff. |
| insert | Splice after target or index 0; assign random id nbformat ≥4.5. Returns insertion message. |
| delete | Splice out cell. Returns unified diff. |

Write via `json.MarshalIndent` (UTF-8, indent=1).

Update ReadFileCache (offset=0 to break Read dedup).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Empty notebook insert | No cell_id → insert at beginning |
| In-place JSON mutation | Non-memoized parse in call() |

## Acceptance Criteria

- **AC1:** Non-ipynb rejected.
- **AC2:** Insert requires cell_type.
- **AC3:** Read + staleness enforced.
- **AC4:** Valid JSON after edit.
- **AC5:** Post-edit Read not file_unchanged stub.
