---
title: Signal Handling
slug: signal-handling
priority: P1
status: done
spec: complete
code: done
package: cmd/jenny
gaps: []
depends_on:
  - cli
---
# Signal Handling

## Overview

The jenny process must respond gracefully to SIGINT (Ctrl+C) and SIGTERM (kill) signals, allowing in-flight API calls to complete and session state to be flushed.

## Motivation

The process previously used a non-cancellable context for the entire agent session. When the user pressed Ctrl+C, Go's default SIGINT handler killed the process without:
1. Waiting for in-flight API calls to complete
2. Flushing session state (cost tracking, transcripts)

## Implementation

The main entry point creates a signal-aware context that cancels on SIGINT (Ctrl+C) or SIGTERM:

- A signal-notify context is derived from `context.Background()`, listening for `os.Interrupt` and `syscall.SIGTERM`
- When Ctrl+C is pressed, the context is cancelled
- The agent loop detects context cancellation at the top of each iteration and returns gracefully
- Pending HTTP requests (using context-aware request construction) are immediately aborted
- The `RunStream` return path flushes cost state and session transcript

**Terminal output after Ctrl+C:**
```
Error: context canceled
```

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Ctrl+C during flag parsing | Go's default handler kills process immediately — acceptable (nothing to clean up) |
| Windows Ctrl+C | `os.Interrupt` is the equivalent — works correctly |
| Double Ctrl+C | Second SIGINT triggers Go's default behavior (immediate kill) — acceptable |
| SIGTERM (`kill <pid>`) | Context cancels, process exits cleanly |

## Portal Unchanged

The `jenny portal` command already has its own signal handler. This fix does not affect portal behavior.

## Acceptance Criteria

- **AC1:** Running `jenny -p "test"` and pressing Ctrl+C (SIGINT) cancels the context. The agent loop detects `ctx.Err()` and returns with `context.Canceled` error. The process exits cleanly within 1 second.
- **AC2:** Running `jenny -p "test"` and sending SIGTERM (`kill <pid>`) cancels the context. The process exits cleanly within 1 second.
- **AC3:** Running `jenny portal` and pressing Ctrl+C still works (portal already has its own signal handler — unchanged).
- **AC4:** `go test ./cmd/jenny/` passes. `go test ./internal/portal/` passes. No behavioral changes during normal (non-interrupted) execution.
