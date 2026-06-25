---
title: Context Soft Pruning
slug: context-soft-pruning
priority: P2
status: not_started
spec: complete
code: not_started
package: internal/agent
depends_on:
  - context-compaction
  - tool-result-spill
  - pinned-messages
---
# Context Soft Pruning

## Problem

Between normal operation and full LLM compaction, there is an opportunity to **reduce context size without calling the LLM** by replacing stale tool result content with lightweight stubs. This "soft pruning" can:

1. Delay or entirely skip LLM compaction (saving cost and latency)
2. Serve as a **final fallback** when LLM compaction itself fails (circuit breaker tripped, API error)

The critical constraint: **prompt cache invalidation must be avoided**. Anthropic caches by byte-prefix; any byte change in an earlier message invalidates the cache for all subsequent messages. Naive in-place pruning destroys cache economics.

## Design

### Two Execution Modes

#### Mode 1: Render-Time Pruning (Cache-Safe)

Applied in a **copy** of the message chain before sending to the API. The persistent transcript is never modified.

- **When**: Every API call, as a `prepareWireMessages` step
- **Scope**: Tool result content in messages **older than the most recent 3 API rounds**
- **Threshold**: Tool results exceeding 4 KB are replaced with a stub
- **Stub format**: `[Pruned: original N chars from <tool_name>. Re-run the tool if needed.]`
- **Exemptions**:
  - Error tool results (`IsError == true`) — always kept in full
  - Pinned messages — never pruned
  - Tool results within the most recent 3 API rounds — fresh context
- **Cache impact**: The pruned copy has a **different byte prefix** than the previous turn's request. This means the first pruned turn is a full cache miss, but subsequent turns re-establish a stable prefix (because the stubs are deterministic and don't change once created). For Anthropic, this means one cache-creation charge followed by cache-read hits.
- **Mitigation**: Only apply render-time pruning when the cache is **already cold** (no `cache_read_input_tokens` in the previous API response, or estimated time since last response exceeds the provider's cache TTL — 5 min for Anthropic). When the cache is warm, skip pruning to preserve the existing cached prefix.

#### Mode 2: Persistent Pruning (Compact Fallback)

Applied to the **actual message chain** when LLM compaction fails and the context must be reduced.

- **When**: After `MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES` (circuit breaker tripped) AND context exceeds `blockingLimit`
- **Scope**: All tool results older than the most recent 2 API rounds
- **Threshold**: 2 KB (more aggressive than render-time)
- **Stub format**: Same as render-time, with `[Persistently pruned: ...]` prefix
- **Persistence**: The pruned messages are written back to transcript as a `prune_boundary` state entry
- **This is a last resort** — the agent can continue operating with degraded context rather than hard-blocking

### Integration Points

#### Pre-API call (render-time)

In the main loop, after compaction check but before `SendMessageStream`:

```
if cacheIsCold && estimatedTokens > warningThreshold:
    wireMessages = renderTimePrune(messages)
```

The function returns a new slice; the original `messages` variable (used for transcript and compaction) is unchanged.

#### Post-compact-failure (persistent)

In the circuit-breaker-tripped branch of the compaction check:

```
if circuitBreakerTripped && estimatedTokens >= blockingLimit:
    messages = persistentPrune(messages)
    // persist prune_boundary entry
```

This replaces the current hard-block behavior with a degraded-but-functional fallback.

### Cache Cold Detection

A helper `isCacheCold` determines whether pruning is safe:

- Track `cache_read_input_tokens` from the most recent API response
- If zero (or the field is absent, e.g., non-Anthropic providers): cache is cold, pruning is safe
- If non-zero: cache is warm, skip render-time pruning
- Alternative: track time since last API response. If > 5 minutes (Anthropic TTL), treat as cold
- For non-Anthropic providers (OpenAI, Google): always treat as cold (no prefix caching benefit to protect)

### Interaction with Other Features

| Feature | Interaction |
|---------|------------|
| **Tool Result Spill** | Spilling happens at execution time (30KB). Soft pruning happens at render/compact time (4KB/2KB). They complement each other: spill prevents giant results from entering the chain; pruning shrinks stale results that were small enough to inline. |
| **Compaction Hysteresis** | The compaction target already accounts for tail pruning. Render-time soft pruning operates independently, before the compaction check. If soft pruning brings tokens below threshold, compaction is skipped entirely. |
| **Pinned Messages** | Pinned messages are exempt from pruning (both modes). |
| **Wire Token Estimation** | Wire tokens should be estimated on the **pruned** copy (render-time mode) to get accurate threshold checks. |

### Acceptance Criteria

- Render-time pruning never modifies the persistent transcript
- Render-time pruning is skipped when cache is warm (non-zero `cache_read_input_tokens`)
- Persistent pruning fires only when the circuit breaker is tripped AND context exceeds blocking limit
- Pinned messages and error tool results are never pruned
- Post-pruning token estimate is lower than pre-pruning estimate
- The stub format includes the original size and tool name for debuggability

### Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Pruning destroys context the model needs | Only prune results older than 3 rounds (render) or 2 rounds (persistent). Recent context is preserved. |
| Cache thrashing from pruning | `isCacheCold` gate ensures pruning only when there's no warm cache to protect |
| Stale data in stubs misleads model | Stub explicitly says "re-run the tool" — the model can recover the data |
| Persistent pruning loses data forever | The spill system already saves large outputs to disk. Pruned content that was previously spilled still exists on disk. For non-spilled content, the transcript entry before the prune boundary contains the original. |
