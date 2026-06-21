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
| **L1** | 10 / 20 consecutive no-progress turns | `<system-reminder>` with structured self-assessment (capability, strategy); fires twice per streak — first at 10 turns, second at 20 | Yes |
| **L2** | 25 consecutive no-progress turns | `<system-reminder>` ordering best-effort output; max_tokens reduced to 4096; allows at most 5 more API turns before escalating to L3 | Yes |
| **L3** | 30 consecutive no-progress turns | Session termination with inability report | No |

All counters reset to 0 when a progress signal (positive turn score) is detected.

## Progress Scoring

Each turn is scored by analyzing tool names, exit codes, command prefixes, and file paths.

### Positive Signals

| Signal | Weight |
|--------|--------|
| File write/edit/notebook_edit to task output (not scratchpad/tmp) | +3 |
| Test pass (bash exit 0 + command matches a known test-runner prefix) | +3 |
| Build success (bash exit 0 + command matches a known build-runner prefix) | +2 |
| Package install (bash exit 0 + install prefix match, or install output detected) | +1 |
| First read of a new file path | +1 |
| Scratchpad write | +1 |

Prefix matching uses word-boundary enforcement: after the prefix, the next character must be end-of-string, space, tab, slash, or equals. This prevents false positives such as `makefile` matching `make` or `go testify` matching `go test`.

Package install is detected two ways:

1. **Prefix match** — command starts with a known installer prefix (e.g. `pip install`, `npm ci`, `apt install`, `cargo install`). See `engine_progress.go` for the full prefix lists (`testRunnerPrefixes`, `buildRunnerPrefixes`, `packageInstallPrefixes`).
2. **Output fallback** — `hasPackageInstallOutput` scans stdout/stderr for: pip (`Successfully installed`), npm (`added` + `packages`), cargo (`Compiling` + ` v`), dpkg (`Setting up`, three or more lines).

Uncategorized successful bash commands (exit 0 but no matching prefix or output pattern) receive no positive score. This is intentional — generic commands like `ls` or `cat` should not reset the counter.

### Negative Signals

| Signal | Weight |
|--------|--------|
| Bash command failed (exit ≠ 0) | -1 |
| Repeated command (same command hash as previous turn) | -2 |
| Repeated error (same error text as previous failure) | -2 |

A turn's score = Σ(signal weights). Score > 0 resets the consecutive counter. Score ≤ 0 increments it.

### Read-path tracking

`previouslyReadPaths` tracks paths already scored as a first read. The map is FIFO-capped at 200 entries; when full, the oldest path is evicted before recording a new one. Evicted paths can score +1 again if re-read later.

## Sliding Window (Secondary Trigger)

A ring buffer of the last 10 turn scores. When the sum ≤ -10 and `slidingL1Fired` is false, L1 fires regardless of the consecutive counter. This prevents circumvention via intermittent low-value actions.

The sliding window can only trigger L1 (never L2 or L3). The `slidingL1Fired` flag is set when the L1 reminder is injected due to a sliding window trigger, and resets on any positive turn (score > 0).

## Level Actions

### L1 — Warning

Injected as a virtual user message. Fires twice per streak:

**First reminder (10 turns):**

```
<system-reminder>
PROGRESS CHECK: No meaningful progress in the last 10 turns. Before continuing, perform a self-assessment:

1. CAPABILITY: Does this task require abilities you lack (vision, specific language runtime, network access, etc.)? If yes, write your best-effort output and stop.
2. STRATEGY: Is your current approach fundamentally flawed? If so, describe an alternative strategy before your next action.

After this assessment:
- If blocked by missing capability: write your best-effort result and stop.
- If a strategy pivot is viable: execute the NEW strategy. Do NOT repeat failed approaches.
- Keep responses concise. Focus on actions, not analysis.
</system-reminder>
```

**Second reminder (20 turns):**

```
<system-reminder>
PROGRESS CHECK (2nd): 20 turns without progress. You should seriously consider stopping.

If you have a viable strategy remaining, execute it now — but be aware that continued lack of progress will result in session termination.

If you are stuck, write your BEST-EFFORT result to the expected output location NOW and stop.
</system-reminder>
```

Resets on progress.

### L2 — Soft Terminate

Injected as a virtual user message:

```
<system-reminder>
FINAL WARNING: 25 turns without progress. You have at most 5 more turns before termination.

Write your BEST-EFFORT result to the expected output location NOW, even if incomplete. A partial answer is better than no answer. Then stop.

Do NOT continue exploring or debugging. Deliver what you have.
</system-reminder>
```

Allows at most 5 additional **API turns** (tracked by `l2TurnCount`, incremented in `CheckAndInject`, which runs once per API call before each request) before escalating to L3. Note: the cap is on API turns, not on individual tool calls — a single turn may execute many parallel tool calls.

### L3 — Hard Terminate

Engine terminates the session and emits a `result` event with `subtype: "progress_terminated"`, including the monitor state (consecutive count, total count, level).

## Integration Points

- **Pre-API check:** Before compaction and normalization, before each API call. Calls `CheckAndInject()` to inject L1/L2 reminders or trigger L3 termination.
- **Post-tool scoring:** After `executeAndProcessTools`. Calls `RecordTurn()` to score the turn and update escalation state, then `SetProgressLevel()` to sync the level to the max_tokens controller.
- **max_tokens coupling:** L1 caps at 8192, L2 caps at 4096, L3 caps at 2048 (via `dynamic-max-tokens`), all with a floor of 2048.

## Invariants

- System prompt is never modified. All injections are virtual user messages.
- L1/L2 reminder text is structured but concise — L1 includes a self-assessment framework, L2 demands best-effort delivery.
- All counters reset on progress (score > 0), including `consecutiveNoProgress`, `level`, `l1FirstFired`, `l1SecondFired`, `l2Fired`, `l2TurnCount`, and `slidingL1Fired`.
- L3 is irreversible once triggered.
- L2 allows at most 5 extra API turns (tracked by `l2TurnCount`, incremented once per `CheckAndInject` call) before escalating to L3.
- Sliding window L1 trigger is guarded by `slidingL1Fired` to prevent repeated firing; resets on positive score.
