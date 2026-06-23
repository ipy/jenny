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

Jenny dynamically adjusts token budgets per API call based on task complexity and session progress. The system computes separate thinking and completion budgets, then maps them to provider-specific parameters.

Two complementary mechanisms work together: a fork-based complexity evaluation (fires once per session after the 3rd tool result) and a turn-based schedule (active from turn 1). Neither modifies the system prompt.

## Fork-Based Complexity Evaluation

After the 3rd tool result, a side request classifies task complexity across four dimensions. The fork reuses the cached system prompt prefix for cache hits.

The fork copies the current message chain, appends a short evaluation instruction, and calls `SendMessage` with a low max_tokens cap (`forkMaxTokens`, 1024). The response is parsed as JSON with four dimension scores (1-5) and a `need_write` flag.

### Dimensions

| Dimension | What it measures | Weight in overall | Primary budget influence |
|-----------|-----------------|-------------------|------------------------|
| **Reasoning depth** | How much deliberation before acting | 30% | Thinking budget |
| **Code volume** | How much code/text to produce | 25% | Completion budget |
| **Domain specialization** | How niche the required knowledge | 20% | Thinking budget |
| **Integration breadth** | How many components/files touched | 25% | Completion budget |

The weighted aggregate is computed internally — the evaluation prompt describes only the dimensions and rubric examples, not the aggregation algorithm.

### Derived Intensities

- **ThinkingIntensity** = 0.60 × reasoning_depth + 0.40 × domain_specialization (biased toward reasoning needs)
- **OutputIntensity** = 0.50 × code_volume + 0.30 × integration_breadth + 0.20 × reasoning_depth (biased toward output volume)

## Token Budget Architecture

Each turn computes a `TokenBudget` with three components:

| Component | Description |
|-----------|-------------|
| `ThinkingBudget` | Tokens allocated for reasoning |
| `CompletionBudget` | Tokens allocated for visible output + tool calls |
| `TotalBudget` | ThinkingBudget + CompletionBudget |

### Provider Mapping

| Provider | Total budget | Thinking control |
|----------|-------------|-----------------|
| **Anthropic** | `max_tokens` = TotalBudget | `thinking.budget_tokens` = ThinkingBudget (per-turn via `SetDynamicThinkingBudget`) |
| **DeepSeek** | `max_tokens` = TotalBudget | Unified budget — no separate control |
| **OpenAI** | `max_completion_tokens` = TotalBudget | Unified budget — no separate control |

`reasoning_effort` is NEVER adjusted dynamically because it would invalidate prompt caches.

### Budget Computation

**Thinking budget** = f(ThinkingIntensity, modelMax, progressLevel):
- Base = intensity × modelMax / 100, floor 1024
- L1: reduced to 70%, L2/L3: reduced to 40%
- Capped at 60% of modelMax

**Completion budget** = f(OutputIntensity, need_write, lastStopReason, lastHadWrite):
- Base = intensity × modelMax / 100, floor 2048
- ×3 multiplier if `need_write` is true
- Spike protection: ×1.5 if previous turn hit `max_tokens`
- Write-phase boost: minimum 8192 if last turn contained write/edit tools
- Capped at modelMax

### Adaptive Signals

| Signal | Effect | Rationale |
|--------|--------|-----------|
| Previous turn hit `max_tokens` | Boost completion ×1.5 | Output was truncated — needs more room |
| Previous turn had write/edit | Completion floor 8192 | Code writing phase needs sustained capacity |
| Progress level L1/L2/L3 | Cap total budget | Stuck sessions should think/write less |

## Turn-Based Fallback

Active from the first turn and when the fork hasn't returned yet.

| Turn Range | max_tokens |
|------------|-----------|
| 1–3 | min(8192, modelMax) for models with ≥100K max output; min(4096, modelMax) otherwise. Split: 30% thinking, 70% completion. |
| 4–10 | min(16384, modelMax) |
| 11+ | Tapers from 16384 by 400/turn, floor 4096 |

Once the fork result is available, it replaces the turn-based schedule for turns > 3.

## Integration with Progress Monitor

When the progress monitor escalates, caps are applied to the total budget:

| Level | Total budget cap |
|-------|-----------------|
| L1 (warning) | min(8192, modelMax), floor 2048 |
| L2 (soft terminate) | min(4096, modelMax), floor 2048 |
| L3 (hard terminate) | min(2048, modelMax), floor 2048 |

Thinking budget is also independently reduced at L1 (70%) and L2/L3 (40%) to encourage action over deliberation.

## Thinking Token Monitor

A separate `ThinkingMonitor` tracks per-turn thinking token usage and injects a `<system-reminder>` when the agent spends disproportionate tokens on reasoning vs. action.

### Scoring Dimensions

| Dimension | Weight | Description |
|-----------|--------|-------------|
| Token/complexity ratio | 35% | thinking_tokens / (complexity × 2000) — disproportionate thinking for task difficulty |
| No-tool-call streak | 30% | Consecutive turns with thinking but no tool calls |
| Early turn penalty | +30% | High thinking (>3000 tokens) in first 3 turns |
| Trend | 15% | Whether thinking is increasing over recent turns |

When the weighted score ≥ 0.7, a virtual user message is injected:

```
<system-reminder>
You are spending too many tokens on internal reasoning relative to actual progress. Prioritize action over analysis:

- Execute tool calls immediately rather than deliberating further
- If you have a reasonable hypothesis, test it with a tool call instead of reasoning about alternatives
- Limit planning to 2-3 sentences before your next action

Act first, analyze results after.
</system-reminder>
```

Capped at 3 reminders per session to avoid noise.

## Invariants

- System prompt blocks are never modified. `CachedSystemPrompt` remains frozen.
- Fork request reuses the identical system prompt prefix as the main session.
- Turns 1–3 always use early-turn cap regardless of fork result.
- `reasoning_effort` is never modified dynamically (cache invalidation risk).
- Thinking + Completion = Total is always maintained after progress caps.
- Total budget is always ≥ 2048 and ≤ modelMax.
