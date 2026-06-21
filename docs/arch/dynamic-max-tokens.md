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

Two complementary mechanisms work together: a fork-based complexity evaluation (fires once per session after the 3rd tool result) and a turn-based schedule (active from turn 1). Neither modifies the system prompt.

## Fork-Based Complexity Evaluation

After the 3rd tool result, a side request classifies task complexity. The fork reuses the cached system prompt prefix for cache hits.

The fork copies the current message chain, appends a short evaluation instruction, and calls `SendMessage` with tools disabled and a low max_tokens cap (200). The response is parsed as JSON: `{"complexity": 1-5, "is_coding": true/false}`.

The fork fires once per session in a goroutine. Main loop does not block. Fork failure is non-fatal — the turn-based schedule applies as fallback.

#### Complexity-Based Calculation

`max_tokens = modelMax × complexity / 100`, floored at 800. Complexity is clamped to [1, 5]. If `is_coding` is true, the result is multiplied by 5 and capped at `modelMax`.

## Turn-Based Dynamic Adjustment

Active from the first turn, independent of the fork.

| Turn Range | max_tokens |
|------------|-----------|
| 1–3 | min(16384, modelMax) for models with ≥100K max output; min(4096, modelMax) otherwise |
| 4–10 | min(16384, modelMax) |
| 11+ | Tapers from 16384 by 400/turn, floor 4096 |

Once the fork result is available, it replaces the turn-based schedule for turns > 3.

## API Layer

`SetMaxTokensOverride(n)` on the `Requester` interface sets a per-call override. Providers pass this to `ResolveMaxTokens(model, override)` instead of using the default (0).

The override is set before each `SendMessageStream` call in the main loop.

## Integration with Progress Monitor

When the progress monitor escalates:

| Level | max_tokens cap |
|-------|---------------|
| L1 (warning) | min(8192, modelMax), floor 2048 |
| L2 (soft terminate) | min(4096, modelMax), floor 2048 |

Caps are applied on top of the base computation (turn-based or complexity-based). The lower of the base value and the progress cap wins.

## Invariants

- System prompt blocks are never modified. `CachedSystemPrompt` remains frozen.
- Fork request reuses the identical system prompt prefix as the main session.
- Turns 1–3 always use early-turn cap (max 16384 for large models, 4096 for small) regardless of fork result.
- `computeMaxTokens` output is always ≥ 800 and ≤ `modelMax`.
- `progressCap` output is always ≥ 2048 and ≤ `modelMax`.
