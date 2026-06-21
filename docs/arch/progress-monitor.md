---
title: Progress Monitor (Circuit Breaker)
slug: progress-monitor
priority: P1
status: done
spec: complete
code: done
package: internal/agent
depends_on:
  - agent-loop
  - query-engine
  - system-prompt
---
# Progress Monitor (Circuit Breaker)

## Overview

Jenny tracks agent progress per turn and intervenes when the session enters an unrecoverable loop. All interventions use virtual user messages — the system prompt is never modified.

## Three-Level Escalation

| Level | Trigger | Action | Reversible |
|-------|---------|--------|------------|
| **L1** | 10 consecutive no-progress turns | `<system-reminder>` with structured self-assessment (capability, knowledge, strategy) | Yes |
| **L2** | 20 consecutive no-progress turns | `<system-reminder>` ordering best-effort output; max_tokens reduced to 4096 | Yes |
| **L3** | 25 consecutive no-progress turns | Session termination with inability report | No |

All counters reset to 0 when a progress signal (positive turn score) is detected.

## Progress Scoring

Each turn is scored by analyzing tool names, exit codes, and file paths.

### Positive Signals

| Signal | Weight |
|--------|--------|
| File write/edit/notebook_edit to task output (not scratchpad/tmp) | +3 |
| Test pass (bash exit 0 + pass/success keywords in output) | +3 |
| Build success (bash exit 0 + compilation keywords) | +2 |
| Package install (bash exit 0 + "successfully installed" in output) | +1 |
| First read of a new file path | +1 |
| Scratchpad write | +1 |

### Negative Signals

| Signal | Weight |
|--------|--------|
| Bash command failed (exit ≠ 0) | -1 |
| Repeated command (same command hash as previous turn) | -2 |
| Repeated error (same error text as previous failure) | -2 |

A turn's score = Σ(signal weights). Score > 0 resets the consecutive counter. Score ≤ 0 increments it.

## Sliding Window (Secondary Trigger)

A ring buffer of the last 10 turn scores. When the sum ≤ -10 and `slidingL1Fired` is false, L1 fires regardless of the consecutive counter. This prevents circumvention via intermittent low-value actions.

The sliding window L1 fires once per escalation cycle. The `slidingL1Fired` flag is set when the L1 reminder is injected due to a sliding window trigger, and resets on any positive turn (score > 0).

## Level Actions

### L1 — Warning

Injected as a virtual user message:

```
<system-reminder>
PROGRESS CHECK: No meaningful progress in the last 10 turns. Before continuing, you MUST perform a self-assessment:

1. CAPABILITY: Does this task require abilities you lack (vision, specific language runtime, network access, etc.)? If yes, stop immediately with your best-effort output.
2. KNOWLEDGE: Are you missing domain knowledge that repeated attempts cannot compensate for? If yes, stop immediately.
3. STRATEGY: Is your current approach fundamentally flawed? If so, describe an alternative strategy before your next action.

After this assessment:
- If blocked by missing capability or knowledge: write your best-effort result and stop.
- If a strategy pivot is viable: describe it briefly, then execute the NEW strategy. Do NOT repeat failed approaches.
- Keep responses concise. Focus on actions, not analysis.
</system-reminder>
```

Fires once per streak. Resets on progress.

### L2 — Soft Terminate

Injected as a virtual user message:

```
<system-reminder>
FINAL WARNING: 20 turns without progress. You have at most 5 more tool calls.

Write your BEST-EFFORT result to the expected output location NOW, even if incomplete. A partial answer is better than no answer. Then stop.

Do NOT continue exploring or debugging. Deliver what you have.
</system-reminder>
```

Allows at most 5 additional tool calls (tracked by `l2TurnCount`) before escalating to L3.

### L3 — Hard Terminate

Engine terminates the session and emits a `result` event with `subtype: "progress_terminated"`, including the monitor state (consecutive count, total count, level).

## Integration Points

- **Pre-API check:** Before compaction and normalization, before each API call. Calls `CheckAndInject()` to inject L1/L2 reminders or trigger L3 termination.
- **Post-tool scoring:** After `executeAndProcessTools`. Calls `RecordTurn()` to score the turn and update escalation state, then `SetProgressLevel()` to sync the level to the max_tokens controller.
- **max_tokens coupling:** L1 caps at 8192, L2 caps at 4096 (via `dynamic-max-tokens`), both with a floor of 2048.

## Invariants

- System prompt is never modified. All injections are virtual user messages.
- L1/L2 reminder text is structured but concise — L1 includes a self-assessment framework, L2 demands best-effort delivery.
- All counters reset on progress (score > 0), including `consecutiveNoProgress`, `level`, `l1Fired`, `l2Fired`, `l2TurnCount`, and `slidingL1Fired`.
- L3 is irreversible once triggered.
- L2 allows at most 5 extra tool calls (tracked by `l2TurnCount`) before escalating to L3.
- Sliding window L1 trigger is guarded by `slidingL1Fired` to prevent repeated firing; resets on positive score.
