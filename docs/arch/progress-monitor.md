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
| **L1** | 10 consecutive no-progress turns | Short `<system-reminder>` for reassessment | Yes |
| **L2** | 20 consecutive no-progress turns | `<system-reminder>` ordering best-effort output; max_tokens reduced to 1000 | Yes |
| **L3** | 25 consecutive no-progress turns | Session termination with inability report | No |

All counters reset to 0 when a progress signal (positive turn score) is detected.

## Progress Scoring

Each turn is scored by analyzing tool names, exit codes, and file paths.

### Positive Signals

| Signal | Weight |
|--------|--------|
| File write to task output (not scratchpad/tmp) | +3 |
| Test pass (bash exit 0 + pass/ok/success in output) | +3 |
| Build success (bash exit 0 + compilation keywords) | +2 |
| Package install | +1 |
| First read of a new file path | +1 |
| Scratchpad write | +1 |

### Negative Signals

| Signal | Weight |
|--------|--------|
| Bash command failed (exit ≠ 0) | -1 |
| Repeated command (same as previous turn) | -2 |
| Repeated error (same error text as previous) | -2 |
| Subagent for infrastructure fix | -3 |

A turn's score = Σ(signal weights). Score > 0 resets the consecutive counter. Score ≤ 0 increments it.

## Sliding Window (Secondary Trigger)

A ring buffer of the last 10 turn scores. When the sum ≤ -10, L1 fires regardless of the consecutive counter. This prevents circumvention via intermittent low-value actions.

The sliding window L1 fires once. After it fires, subsequent positive turns do not re-arm it until the window naturally fills with new data.

## Level Actions

### L1 — Warning

Injected as a virtual user message:

```
<system-reminder>
PROGRESS CHECK: No progress in the last 10 turns. Reassess your approach or stop if blocked.
</system-reminder>
```

Fires once per streak. Resets on progress.

### L2 — Soft Terminate

Injected as a virtual user message:

```
<system-reminder>
FINAL: 20 turns without progress. Write your best-effort result now and stop.
</system-reminder>
```

Allows at most 5 additional turns before escalating to L3.

### L3 — Hard Terminate

Engine terminates the session and emits a `result` event with `subtype: "progress_terminated"`, including the monitor state (consecutive count, total count, level).

## Integration Points

- **Pre-API check:** After `drainTaskCompletions`, before `normalizeNewMessage`. Injects L1/L2 reminders or executes L3 termination.
- **Post-tool scoring:** After `executeAndProcessTools`. Records the turn and updates the max_tokens controller's progress level.
- **max_tokens coupling:** L1 caps at 2000, L2 caps at 1000 (via `dynamic-max-tokens`).

## Invariants

- System prompt is never modified. All injections are virtual user messages.
- L1/L2 reminder text is short (≤ 2 lines) to avoid attention disruption.
- All counters reset on progress (score > 0).
- L3 is irreversible once triggered.
