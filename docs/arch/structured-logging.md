---
title: Structured Logging
slug: structured-logging
priority: P3
status: done
spec: complete
code: done
package: internal/log
gaps: []
depends_on:
  []
---
# Structured Logging

## Overview

Jenny uses structured logging to stderr. Debug gated on env/flag; diagnostics ring buffer for headless troubleshooting.

## Current Implementation

| Control | Behavior |
|---------|----------|
| `JENNY_DEBUG=1` | DEBUG level slog to stderr |
| unset | INFO and above |
| `--verbose` | Same as debug enable |

All logs to **stderr**; stdout reserved for agent output / stream-json.

## Error Ring Buffer (target)

- Max **100** entries `{ error, timestamp }`
- FIFO eviction
- `getInMemoryErrors()` for diagnostics export

## Last-Request Capture (target)

- Store API params **without messages** for all sessions
- Optional full messages for internal debug only
- Main-thread query source only
- Overwritten each turn

## MCP Channels

Error/debug channels from MCP clients drained on attach.

## Headless Policy

No persistent error files unless explicitly configured. Ring buffer always on in memory.

## Implementation Details

### Thread Safety & Immutability
- **Deep Copying:** Functions returning internal state slices (e.g., `GetLastRequest()`) MUST return deep-copied slices to prevent external mutation of the internal store.
- **Mutex Protection:** All global state (`errorRing`, `lastRequestStore`) must be protected by a dedicated mutex.

## Testing Standards

### Parallel-Safe Tests
- Tests must not mutate global state directly.
- Use `ResetForTest()` to initialize/reset the package state.
- Always use `t.Cleanup(ResetForTest)` to ensure state is cleared after each test run, allowing for safer parallel execution.

## Acceptance Criteria

- **AC1:** Logs never on stdout in stream-json mode.
- **AC2:** JENNY_DEBUG enables DEBUG level.
- **AC3:** Ring buffer caps at 100 entries.
- **AC4:** Last-request capture excludes messages by default.
- **AC5:** Verbose flag equivalent to debug enable.
