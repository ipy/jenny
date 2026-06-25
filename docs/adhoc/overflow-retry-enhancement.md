---
title: Overflow Retry Enhancement
slug: overflow-retry-enhancement
priority: P2
status: not_started
spec: complete
code: not_started
package: internal/agent
depends_on:
  - context-compaction
  - context-soft-pruning
---
# Overflow Retry Enhancement

## Problem

Jenny handles API context overflow in several places, but the logic is fragmented and some paths terminate the session when recovery is possible:

1. **`context_exhausted` from streaming** (`engine_stopreasons.go`): Attempts `emergencyCompact`, continues if successful. Good path.
2. **`context_exhausted` from pre-stream HTTP 400** (`engine_loop.go`): Also attempts `emergencyCompact`. Good path.
3. **`prompt too long` from compaction itself** (`compact.go`): Retries with 25% message dropping, up to 5 attempts. Good path.
4. **Circuit breaker tripped + context near limit**: Currently hard-blocks with `PromptTooLongError`. No recovery attempted.
5. **HTTP 413 from some providers**: May not be recognized as a recoverable overflow error.

The goal is a unified, multi-stage overflow recovery pipeline that maximizes the chance of continuing the session.

## Proposed Recovery Pipeline

When an overflow condition is detected (from any source), apply these stages in order:

### Stage 1: Soft Prune (zero LLM cost)

If not already applied, run persistent pruning (see `context-soft-pruning.md`):
- Replace stale tool results (>2 rounds old, >2KB) with stubs
- Re-estimate tokens
- If below threshold → retry the API call

### Stage 2: In-Session Compact (cache-friendly)

If soft pruning wasn't enough:
- Attempt `inSessionCompact` (shares cache prefix)
- If successful → retry
- If fails (prompt too long for compaction itself) → Stage 3

### Stage 3: Aggressive Fallback Compact

If in-session compact fails:
- Use `fallbackSummaryCompact` with progressive message dropping (existing behavior)
- Drop 25% per retry, up to 5 attempts
- If successful → retry

### Stage 4: Emergency Compact

If fallback compact fails:
- Use `emergencyCompact` (keep first + last 10 messages, mechanical summary)
- If successful → retry

### Stage 5: Hard Block

If all stages fail:
- Emit `error_max_tokens` with `context_exhausted` category
- Terminate the session

### Error Recognition

Unify overflow error detection. Currently `isPromptTooLongError` checks for several strings. Add:
- HTTP 413 status code
- Provider-specific error codes: `context_length_exceeded` (OpenAI), `RESOURCE_EXHAUSTED` (Google)
- The check should be in a single function used by all providers

### Circuit Breaker Integration

The current circuit breaker trips after 3 consecutive compact failures and then **permanently blocks auto-compact** for the session. With the new pipeline:

- Circuit breaker still tracks consecutive failures
- When tripped: skip auto-compact at the normal threshold, but **still attempt recovery when overflow actually occurs** (stages 1-5)
- Rationale: the circuit breaker prevents wasteful proactive compaction, but when the API actually rejects a request, recovery must be attempted regardless

### Retry Semantics

When overflow recovery succeeds:
- The retry does NOT count against `maxIterations` or `turnCount`
- The retry uses the same `messages` (now compacted) and re-invokes `SendMessageStream`
- Persist a `state` entry to the transcript indicating the recovery (type, pre/post token counts)

### Token Budget Checks

With wire-equivalent token estimation, the pre-API check should catch most overflow conditions before they reach the API. However, the wire estimate can still underestimate (EWMA hasn't converged yet, or tool schemas changed). The recovery pipeline handles the residual cases.

## Acceptance Criteria

- Overflow from any provider (Anthropic, OpenAI, Google) triggers the same recovery pipeline
- HTTP 413 is recognized as a recoverable overflow condition
- Soft pruning is attempted before any LLM-based compaction
- Circuit breaker prevents proactive compaction but allows reactive recovery
- Recovery retries do not consume turn/iteration counters
- Each recovery stage is logged with pre/post token counts
- The pipeline terminates deterministically (no infinite retry loops)

## Interaction with Existing Features

| Feature | Interaction |
|---------|------------|
| **Emergency compact** | Becomes Stage 4 instead of the only recovery mechanism |
| **Circuit breaker** | Blocks proactive compaction, allows reactive recovery |
| **Wire token estimation** | Better pre-API estimates reduce how often overflow recovery is needed |
| **Soft pruning** | New zero-cost Stage 1 before LLM compaction |
| **output_cap_hit retry** | Orthogonal — that's about output truncation, not input overflow |
