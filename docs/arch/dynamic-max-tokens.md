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
  - side-query
---
# Dynamic max_tokens

## Overview

Jenny dynamically adjusts token budgets per API call based on task complexity and session progress. The system uses a **fixed thinking budget** and a **variable completion budget**, then maps them to provider-specific parameters.

**Critical invariant:** The thinking budget (`budget_tokens` for Anthropic) is determined once at session start and never changes. Anthropic's cache key includes thinking parameters, so any change to `budget_tokens` invalidates the entire cached prefix. Instead of modifying the budget, thinking intensity is controlled via prompt-based guidance hints (see Thinking Guidance Hints below).

Two complementary mechanisms work together: a fork-based complexity evaluation (fires once per session after the 3rd tool result, dispatched via the SideQuery framework) and a turn-based schedule (active from turn 1). Neither modifies the system prompt.

## Fork-Based Complexity Evaluation

After the 3rd tool result, a side query classifies task complexity across four dimensions. The fork is dispatched via the SideQuery framework (`dispatchComplexityEval`) which reuses the cached system prompt prefix for cache hits.

The fork copies the current message chain, appends a short evaluation instruction, and calls `SendMessage` with a low max_tokens cap (`forkMaxTokens`, 1024). The response is parsed as JSON with four dimension scores (1-5) and a `need_write` flag.

### Dimensions

| Dimension | What it measures | Weight in overall | Primary budget influence |
|-----------|-----------------|-------------------|------------------------|
| **Reasoning depth** | How much deliberation before acting | 30% | Thinking guidance hints |
| **Code volume** | How much code/text to produce | 25% | Completion budget |
| **Domain specialization** | How niche the required knowledge | 20% | Thinking guidance hints |
| **Integration breadth** | How many components/files touched | 25% | Completion budget |

The weighted aggregate is computed internally — the evaluation prompt describes only the dimensions and rubric examples, not the aggregation algorithm.

### Derived Intensities

- **ThinkingIntensity** = 0.60 × reasoning_depth + 0.40 × domain_specialization (used for prompt-based guidance, not budget calculation)
- **OutputIntensity** = 0.50 × code_volume + 0.30 × integration_breadth + 0.20 × reasoning_depth (biased toward output volume)

## Token Budget Architecture

Each turn computes a `TokenBudget` with three components:

| Component | Description |
|-----------|-------------|
| `ThinkingBudget` | **Fixed** — set once at session start, never changes |
| `CompletionBudget` | **Variable** — adjusted per turn based on complexity, progress, and adaptive signals |
| `TotalBudget` | ThinkingBudget + CompletionBudget |

### Fixed Thinking Budget

Computed once by `computeFixedThinkingBudget`:
- Value = 60% of `modelMax` (model's maximum output tokens)
- Floor: 1024 tokens
- Can be overridden by `--thinking-budget` CLI flag (still fixed for the session)

The model self-regulates actual thinking usage within this budget. The large allocation ensures deep reasoning is always available without requiring dynamic adjustment.

### Provider Mapping

| Provider | Total budget | Thinking control |
|----------|-------------|-----------------|
| **Anthropic** | `max_tokens` = TotalBudget | `thinking.budget_tokens` = FixedThinkingBudget (set once, never changes) |
| **DeepSeek** | `max_tokens` = TotalBudget | Unified budget — no separate control |
| **OpenAI** | `max_completion_tokens` = TotalBudget | Unified budget — no separate control |

Neither `reasoning_effort` nor `budget_tokens` is adjusted dynamically — both cause cache invalidation.

### Completion Budget Computation

**Completion budget** = f(OutputIntensity, need_write, lastStopReason, lastHadWrite, progressLevel):
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
| Progress level L1/L2/L3 | Cap **completion** budget only | Stuck sessions should write less; thinking budget is immutable |

## Thinking Guidance Hints

Instead of modifying `budget_tokens`, thinking intensity is controlled via transient `<system-reminder>` messages injected before each API call and stripped immediately after. These hints do not persist in the conversation history and do not break prompt caching because they are appended at the end of the message list.

`ThinkingGuidanceHint(turn)` returns a hint based on:

| Condition | Hint |
|-----------|------|
| Turn ≤ 2 | "Think briefly — focus on understanding the task" |
| Progress ≥ L2 | "Think very briefly about what is fundamentally different, then act" |
| Progress ≥ L1 | "Reflect briefly on why approaches are failing, then pivot" |
| ThinkingIntensity ≤ 2 | "Straightforward task — keep thinking concise" |
| Default | No hint (empty string) |

## Turn-Based Fallback

Active from the first turn and when the fork hasn't returned yet.

| Turn Range | Completion budget |
|------------|------------------|
| 1–3 | min(4096, modelMax) for models with ≥100K max output; min(2048, modelMax) otherwise |
| 4–10 | 60% of min(16384, modelMax) |
| 11+ | 60% of tapered value (16384 → floor 4096, -400/turn) |

The thinking budget is always the fixed value regardless of turn number.

Once the fork result is available, it replaces the turn-based schedule for turns > 3 (completion budget only).

## Integration with Progress Monitor

When the progress monitor escalates, caps are applied to the **completion budget only**:

| Level | Completion budget cap |
|-------|-----------------------|
| L1 (warning) | 8192 |
| L2 (soft terminate) | 4096 |
| L3 (hard terminate) | 2048 |

The thinking budget is never capped or reduced — it remains fixed for the entire session.

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

- **Thinking budget is fixed for the entire session** — never modified per turn.
- System prompt blocks are never modified. `CachedSystemPrompt` remains frozen.
- Side queries (complexity eval, progress eval) use the SideQuery framework, sharing the exact same cache prefix as the main session (model, thinking config, system prompt, tools).
- Turns 1–3 always use early-turn completion cap regardless of fork result.
- `reasoning_effort` and `budget_tokens` are never modified dynamically (cache invalidation risk).
- Thinking + Completion = Total is always maintained after progress caps.
- Total budget is always ≥ 2048 and ≤ modelMax.
- Minimum completion budget is 1024 tokens.
