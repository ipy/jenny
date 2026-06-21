---
title: Dynamic max_tokens
slug: dynamic-max-tokens
priority: P1
status: done
spec: complete
code: done
package: internal/agent, internal/api
depends_on:
  - context-compaction
  - system-prompt
  - query-engine
---
# Dynamic max_tokens

## Overview

Jenny dynamically adjusts `max_tokens` per API call based on task complexity and session progress. This controls thinking volume for models where `max_tokens` is the only lever (no separate thinking budget parameter).

Two complementary mechanisms work together. Neither modifies the system prompt.

## Fork-Based Complexity Evaluation

After the 3rd tool result, a side request classifies task complexity. The fork reuses the cached system prompt prefix for cache hits.

The fork copies the current message chain, appends a short evaluation instruction, and calls `SendMessage` with tools disabled and a low max_tokens cap (200). The response is parsed as JSON: `{"complexity": 1-5, "is_coding": true/false}`.

The fork fires once per session in a goroutine. Main loop does not block. Fork failure is non-fatal — the turn-based schedule applies as fallback.

### Complexity-Based Calculation

`max_tokens = modelMax × complexity / 100`, floored at 800. If `is_coding` is true, the result is multiplied by 5 and capped at `modelMax`.

## Turn-Based Dynamic Adjustment

Active from the first turn, independent of the fork.

| Turn Range | max_tokens |
|------------|-----------|
| 1–3 | 2000 (discovery) |
| 4–10 | 8000 |
| 11+ | Tapers from 8000 by 200/turn, minimum 2000 |

Once the fork result is available, it replaces the turn-based schedule for turns > 3.

## API Layer

`SetMaxTokensOverride(n)` on the `Requester` interface sets a per-call override. Providers pass this to `ResolveMaxTokens(model, override)` instead of using the default (0).

The override is set before each `SendMessageStream` call in the main loop.

## Integration with Progress Monitor

When the progress monitor escalates:

| Level | max_tokens cap |
|-------|---------------|
| L1 (warning) | 2000 |
| L2 (soft terminate) | 1000 |

## Invariants

- System prompt blocks are never modified. `CachedSystemPrompt` remains frozen.
- Fork request reuses the identical system prompt prefix as the main session.
- Turns 1–3 always use max_tokens ≤ 2000 regardless of fork result.
- `computeMaxTokens` output is always ≥ 800 and ≤ `modelMax`.
