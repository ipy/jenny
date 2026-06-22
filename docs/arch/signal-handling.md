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

The jenny process must respond to SIGINT (Ctrl+C) and SIGTERM signals by cancelling the in-flight agent loop and exiting cleanly. A second SIGINT (double Ctrl+C) must escalate to immediate forced termination so the user is never stuck waiting for an unresponsive process.

## Motivation

Two failure modes have been observed:

1. **Single Ctrl+C during `tool_use` turn**: a synthetic-result detour was added to deliver "Tool execution interrupted" feedback to the model. The detour accidentally stripped the cancellable signal context (`context.WithoutCancel`), so a subsequent iteration of the loop could not detect cancellation and the process kept running. Multiple Ctrl+C presses were ignored.
2. **No escalation**: the user had no way to force the process to die if the cooperative cancellation did not unwind within a reasonable time.

## Implementation

The main entry point creates a signal-aware context that cancels on the first SIGINT (Ctrl+C) or SIGTERM, AND installs an escalating handler that calls `os.Exit(2)` on a second SIGINT:

- A signal-notify context is derived from `context.Background()`, listening for `os.Interrupt` and `syscall.SIGTERM` — this is the cooperative cancel.
- A second `signal.Notify` channel listens for `os.Interrupt` only — this is the escalation channel. On its first receive, the handler calls `os.Exit(2)` immediately (no defer, no flush).
- Both handlers are torn down with `defer stop()` so the runtime does not leak handlers.

When the cooperative context cancels, the engine loop unwinds as follows:

- **Top of loop (`ctx.Err() != nil`)**: if cancelled, return `context.Canceled`. This is the fast path that the `WithoutCancel` detachment must not break.
- **`handleStopToolUse`**: receives `ctx`. If the original context is already cancelled (i.e. user pressed Ctrl+C during the tool turn), it returns `shouldContinue = false` so the loop terminates even when all tool results are synthetic. This prevents the model from re-entering the loop with new tool calls.
- **Synthetic-result context detach**: limited to a single iteration. A `syntheticInFlight` flag on the engine is set when `hasSynthetic` is true; the loop runs exactly one more iteration to deliver the synthetic results, then returns. The flag is reset only when the iteration completes with non-synthetic results or the loop exits.

Pending HTTP requests built with `http.NewRequestWithContext(ctx, ...)` abort immediately on cancellation. The `RunStream` return path flushes cost state and session transcript.

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Ctrl+C during flag parsing | Go's default handler kills process immediately — acceptable (nothing to clean up) |
| Windows Ctrl+C | `os.Interrupt` is the equivalent — works correctly |
| Single Ctrl+C during agent turn | Cooperative cancel: tools abort, loop returns `context.Canceled`, process exits within 1s |
| **Double Ctrl+C** | Second SIGINT calls `os.Exit(2)` immediately — user is never trapped |
| Ctrl+C during `tool_use` turn (synthetic-result path) | Loop runs one more iteration to deliver the synthetic tool_results to the model, then exits |
| SIGTERM (`kill <pid>`) | Context cancels (no escalation), process exits cleanly within 1s |

## Portal Unchanged

The `jenny portal` command already has its own signal handler. This fix does not affect portal behavior.

## Acceptance Criteria

- **AC1:** Running `jenny -p "test"` and pressing Ctrl+C (SIGINT) cancels the context. The agent loop detects `ctx.Err()` and returns with `context.Canceled` error. The process exits cleanly within 1 second.
- **AC2:** Running `jenny -p "test"` and sending SIGTERM (`kill <pid>`) cancels the context. The process exits cleanly within 1 second.
- **AC3:** Running `jenny portal` and pressing Ctrl+C still works (portal already has its own signal handler — unchanged).
- **AC4:** `go test ./cmd/jenny/` passes. `go test ./internal/portal/` passes. No behavioral changes during normal (non-interrupted) execution.
- **AC5:** Pressing Ctrl+C twice within 5 seconds during a `tool_use` turn causes the process to exit with status code 2 within 100ms of the second press, even if the cooperative cancel has not unwound.
- **AC6:** Pressing Ctrl+C during a `tool_use` turn terminates the loop after at most one additional iteration (the synthetic-result delivery iteration). The process does not loop indefinitely.
