---
title: Session Memory
slug: session-memory
priority: P3
status: done
spec: complete
code: done
package: internal/agent
gaps:
  - "Natural break detection: not yet implemented"
depends_on:
  - context-compaction
---
# Session Memory

## Overview

Background markdown notes file maintained by a standalone API call with Edit-only tool access. Updates incrementally as session grows.

## Thresholds (defaults)

| Event | Threshold |
|-------|-----------|
| Init | ~15K context tokens (when file doesn't exist yet) |
| Update | Every ~8K token growth **and** 3 tool calls since last baseline |

**Planned (not yet implemented):** Natural break detection (~5K tokens when last assistant has no pending tool calls).

Token counting matches autocompact: input + output + cache tokens.

> **Concern (2026-06-15):** Current mechanism is "threshold-triggered + LLM decides if change is needed". This means the "Session memory updated" log can be misleading — the update may be triggered but the LLM may determine no new context warrants an edit, resulting in no file change despite the log. Current fix: log level distinguishes actual edits (`Info`) from no-change cases (`Debug`). Long-term consideration: consider triggering on semantic "key decision points" rather than fixed token thresholds, or require LLM to produce a confirmation edit even when deciding no changes are needed.

## Extraction

- Wait timeout: **15s**
- Stale in-flight (>60s): do not wait
- Threshold baselining uses `lastBaseline` (token count) and `lastToolBaseline` (tool call count), reset after each update

## Forked Agent Constraints

- May **Edit only** the session memory file.
- Uses standalone `SendMessage` API call (not a forked sub-agent with shared prompt cache).
- Gated on auto-compact enabled AND `EnableSessionMemory` flag (`--enable-session-memory` / `JENNY_ENABLE_SESSION_MEMORY`). Both must be true for session memory to activate; either false → disabled.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| First run | Create file with template (mode 0600) |
| Read dedup | Seed `readCache` via `RecordRead` before edit |

## Acceptance Criteria

- **AC1:** Init at ~15K tokens.
- **AC2:** Update respects token + tool call thresholds.
- **AC3:** 15s extraction timeout.
- **AC4:** Forked agent Edit-only on memory file.
- **AC5:** Disabled when auto-compact off.
- **AC6:** Disabled when `EnableSessionMemory` is false (default). Must not create, init, or update the session memory file unless explicitly enabled.
