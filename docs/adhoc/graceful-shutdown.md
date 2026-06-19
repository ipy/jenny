---
title: Graceful Shutdown on Ctrl+C
slug: graceful-shutdown
status: open
date: 2026-06-19
---

# Graceful Shutdown on Ctrl+C

Ctrl+C takes an unreasonably long time to terminate jenny during task execution.
This document traces the full shutdown path from SIGINT delivery to process exit,
identifies every blocking point, and proposes a unified fix.

## Root-Cause Analysis

### R1: SSE stream loops ignore context cancellation

The primary blocker. All four providers (Anthropic, OpenAI, OpenAI-Responses, GenAI)
share the same pattern:

```
for {
    data, ok := scanner.Next()   // blocks on bufio.Scanner.Scan()
    if !ok { break }
    // process data, reset idle timer
}
```

`SSEScanner.Next()` calls `bufio.Scanner.Scan()`, which blocks on `io.Read`
from the HTTP response body. The loop never checks `ctx.Done()`. When Ctrl+C
cancels the context, the only way the loop breaks is when the idle watchdog
calls `body.Close()` — but `DefaultIdleTimeout` is 30 seconds.

**Worst case**: Ctrl+C during an active stream → 30-second wait before idle
watchdog fires → `body.Close()` → scanner returns false → loop exits.

**Affected files** (all identical pattern):
- `internal/api/provider_anthropic.go:527-619`
- `internal/api/provider_openai.go:520-541`
- `internal/api/provider_openai_responses.go:548+`
- `internal/api/provider_genai.go:356-388`

### R2: Dual SIGINT handler conflict

Two independent signal registrations compete for SIGINT:

1. `session.Manager.RegisterShutdownFlush()` (line 566 of `manager.go`) calls
   `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` in a goroutine that
   waits for a signal, flushes transcript writes, and closes a done channel.

2. `cmd/jenny/main.go:534` calls `signal.NotifyContext()` to create the
   cancellation context passed to `RunStream`.

Go's `signal.Notify` is multicast — both handlers receive SIGINT. The
`RegisterShutdownFlush` goroutine blocks on `<-sigCh` and then calls `m.Flush()`.
This is harmless if the main path is already unblocked, but if the main path is
stuck on R1, the flush completes while the process is still waiting, and the
done channel is never consumed by anyone — the goroutine leaks.

### R3: MCP ShutdownAll may block on unresponsive child processes

`mcp.ShutdownAll()` is called via `defer` in `run()`. It calls
`client.Disconnect()` → `cleanup()` for each MCP client. `cleanup()` writes a
`notifications/shutdown` JSON-RPC message to the child's stdin, then closes
stdin. If the child process is stuck or ignoring stdin, `stdin.Write` or
`stdin.Close` may block. There is no timeout on this cleanup path.

### R4: Bash tool child processes escape the process group

`exec.CommandContext` sends SIGKILL to the direct child process on context
cancellation, but `sh -c <command>` often spawns grandchildren. Without
`SysProcAttr.Setpgid`, those grandchildren remain in the parent's process group
and are not terminated. `cmd.Wait()` blocks until all file descriptors are
closed — if a grandchild holds stdout/stderr pipes open, the wait hangs
indefinitely.

### R5: Fallback path reuses the cancelled context (functional defect)

In `client.go:290`, the fallback path creates
`context.WithTimeout(ctx, fallbackTimeout)` where `ctx` is already cancelled.
This means the fallback timeout is effectively zero — it returns immediately
with a context error. The intent was to retry with a fresh request, but the
cancelled parent context makes this impossible. This is not a blocking issue (the process does not hang), but it means the
fallback can never succeed after Ctrl+C.

### R6: Memory extraction goroutines outlive the main loop

`MemoryExtractor.CheckAndExtract()` fires extraction in a background goroutine
with `context.Background()` (not the cancellation context). When the main loop
exits due to Ctrl+C, the extraction goroutine may still be running an API call
— which is itself blocked on R1. `engine.Drain()` is never called from
`cmd/jenny/main.go`, so there is no coordinated shutdown of these goroutines.

## Proposed Solution

The fix has three layers: a context-aware stream reader, unified signal handling,
and bounded cleanup.

### L1: Context-aware SSE scanner

Replace `SSEScanner` with a reader that respects context cancellation. The
approach: wrap the blocking `Read` in a goroutine, and `select` between the
read result and `ctx.Done()`.

```go
type ctxSSEScanner struct {
    ch     chan scanResult
    cancel context.CancelFunc
}

type scanResult struct {
    data string
    ok   bool
}

func newCtxSSEScanner(ctx context.Context, r io.Reader) *ctxSSEScanner {
    readCtx, cancel := context.WithCancel(ctx)
    s := &ctxSSEScanner{
        ch:     make(chan scanResult, 1),
        cancel: cancel,
    }
    inner := NewSSEScanner(r)
    go func() {
        defer close(s.ch)
        for {
            data, ok := inner.Next()
            select {
            case s.ch <- scanResult{data, ok}:
                if !ok { return }
            case <-readCtx.Done():
                return
            }
        }
    }()
    return s
}

func (s *ctxSSEScanner) Next(ctx context.Context) (string, bool) {
    select {
    case r, ok := <-s.ch:
        if !ok { return "", false }
        return r.data, r.ok
    case <-ctx.Done():
        s.cancel() // unblock the read goroutine
        return "", false
    }
}
```

Each provider's streaming loop changes from `scanner.Next()` to
`scanner.Next(ctx)`. When Ctrl+C cancels the context, `Next(ctx)` returns
immediately, the loop breaks, `defer body.Close()` runs, and the read goroutine
unblocks and exits.

**Why this approach over alternatives:**

- **vs. `body.Close()` in a goroutine**: The idle watchdog already does this,
  but with a 30-second delay. We could add a separate goroutine that closes
  `body` on `ctx.Done()`, but that's a race — closing the body while
  `bufio.Scanner` is mid-read causes a data race on the scanner's internal
  buffer. The channel-based approach serializes: the read goroutine owns the
  scanner exclusively, and the consumer only reads from the channel.

- **vs. `SetReadDeadline` on the connection**: Would require access to the
  underlying `*net.TCPConn` from `http.Response.Body`, which is fragile
  (proxy/tunnel connections wrap the body). Also doesn't solve the problem if
  the server is actively sending keep-alive SSE events.

- **vs. Replacing `bufio.Scanner` with `bufio.Reader` + manual line parsing**:
  Possible, but more invasive. The channel approach reuses the existing
  `SSEScanner` as-is.

### L2: Merge signal handlers into one

Remove `RegisterShutdownFlush()` as a signal handler. Instead:

1. `signal.NotifyContext` in `main.go` remains the single source of truth for
   SIGINT/SIGTERM.
2. After `RunStream` returns (whether due to cancellation or completion), call
   `sessionManager.Flush()` explicitly in the cleanup path of `run()`.

This eliminates the dual-handler confusion and ensures flush always happens,
even on non-signal exit paths (e.g., budget exceeded, max turns).

### L3: Bounded cleanup

Three changes to ensure the process exits within a bounded time after the
stream loop breaks:

1. **MCP shutdown with timeout**: Wrap `mcp.ShutdownAll()` in a goroutine with
   a 5-second timeout. If cleanup doesn't complete, log a warning and proceed.
   The OS will reap the child processes when jenny exits.

2. **Bash tool process groups**: Set `SysProcAttr{Setpgid: true}` on all
   `exec.CommandContext` calls in `bash.go` for Unix, using the same build-tag
   pattern already in `internal/portal/process_unix.go` /
   `process_windows.go`. On context cancellation, kill the entire process group
   (`syscall.Kill(-pid, SIGKILL)`) instead of just the direct child. The
   Windows variant (`internal/tool/signal_windows.go`) already uses
   `taskkill /F /T` for process-tree termination; the bash tool should follow
   the same pattern.

3. **Memory extraction drain with timeout**: Call `engine.Drain(ctx)` in
   `run()` after `RunStream` returns, with a 3-second deadline. The drain
   already has timeout protection internally, but the outer context ensures
   we don't wait longer than necessary.

## Acceptance Criteria

| AC | Requirement |
|----|-------------|
| AC1 | After Ctrl+C, the SSE stream loop exits within 2 seconds; the process exits within 2 seconds under normal cleanup, and within 8 seconds in the worst case (MCP cleanup timeout) |
| AC2 | Transcript data is flushed to disk before exit regardless of exit path |
| AC3 | MCP child processes are terminated (not orphaned) on exit |
| AC4 | Bash tool grandchildren are terminated on context cancellation |
| AC5 | Streaming fallback is not attempted when the parent context is already cancelled |
| AC6 | Memory extraction goroutines are drained with bounded timeout on exit |

## Migration Path

1. Implement `ctxSSEScanner` in `internal/api/http.go` alongside existing `SSEScanner`.
2. Update all four providers to use `ctxSSEScanner` — mechanical change in each
   streaming loop.
3. Remove `RegisterShutdownFlush()` signal handler; add explicit `Flush()` call
   in `run()` cleanup.
4. Add `SysProcAttr` configuration to bash tool commands following the
   build-tag pattern in `internal/portal/process_unix.go` and
   `process_windows.go`; kill process group on cancellation.
5. Add timeout wrapper around `mcp.ShutdownAll()`.
6. Add `engine.Drain(drainCtx)` call with 3s deadline in `run()`.
7. Skip streaming fallback when `ctx.Err() != nil` in `client.go`.
8. Delete `RegisterShutdownFlush` method from `session.Manager`.