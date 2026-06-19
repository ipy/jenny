---
title: Memory Extraction
slug: memory-extraction
priority: P3
status: done
spec: partial
code: done
package: internal/agent
gaps: []
depends_on:
  - memdir
  - tool-registry
---
# Memory Extraction

## Overview

End-of-turn extraction agent extracts durable memories to auto-memory directory. Uses a single `SendMessage` API call (not a multi-turn loop or forked subagent).

## Timing

- Run at stop hooks when no pending tool calls.
- Main agent only (not subagents).
- Throttle: every N eligible turns (default 1).

## Extraction Model

The extractor calls `client.SendMessage()` directly with a fresh message containing the extraction prompt and conversation context. This is a single-shot API call with no transcript involvement.

## Mutual Exclusion

If main agent wrote to auto-mem paths since cursor → skip extraction, advance cursor only.

## Cursor

- `lastMemoryMessageUuid`
- UUID missing after compaction → count all model-visible messages (do not permanently disable)

## Coalescing

In-progress runs stash latest context for one trailing run.

## Permissions (extraction tools)

- Read/Grep/Glob unrestricted
- Edit/Write only under auto-mem dir
- No Bash tool provided

## Shutdown

Drain with 15s default extraction timeout. The main process calls drain on exit (normal, Ctrl+C, budget exceeded, or max turns) with a bounded deadline. `Drain` respects the caller's context deadline.

Pre-inject memory manifest to avoid extra ls turn.

## Acceptance Criteria

- **AC1:** Runs end-of-turn only.
- **AC2:** Skips when main agent wrote memory in range.
- **AC3:** Compaction cursor fallback by message count.
- **AC4:** Edit scoped to auto-mem dir.
- **AC5:** Coalesces concurrent extraction requests.
