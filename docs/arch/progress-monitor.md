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

## L1-Second Fork-Judge Evaluation

Before injecting the L1 second reminder (at 20 turns), the engine fires a non-blocking side query ("fork") to evaluate whether the agent is making genuine progress that the heuristic scoring missed. This prevents false-positive escalation for task categories invisible to lexical scoring, and intervenes earlier than L2 to give the agent more room to recover.

### Motivation

Heuristic scoring (`scoreTurn`) only rewards a fixed set of signals: file writes, known test/build/install commands, and first-time reads. Many legitimate workflows produce none of these:

- **SQL tuning / data analysis**: commands are `SELECT`, `EXPLAIN`, etc. — never in the scoring prefix lists
- **Infrastructure / DevOps**: `kubectl`, `terraform`, `docker`, `helm`, `git` operations
- **Monitoring / debugging**: `curl`, `grep` through logs, `journalctl`
- **Text exploration**: `jq`, `awk`, `sed`, `sort | uniq -c` pipelines
- **User-requested repetition**: "check all 50 configs for X", "run this benchmark 10 times"
- **Stochastic tasks**: fuzzing, hyperparameter search, A/B testing — inherently no convergent output

### Timing

The fork fires when the consecutive no-progress counter reaches the L1-second threshold (`l1SecondThreshold`, currently 20). L1-first (10 turns) is too early — exploration is still expected. Waiting until 20 turns gives the judge enough history while leaving 5 turns before L2 (25 turns) for the agent to self-correct if the judge result is negative.

### Judge Prompt

The fork copies the full conversation history and appends the judge prompt as a final user message. This preserves the shared prefix (system prompt + prior messages) for prompt cache hits.

The judge prompt asks the model to evaluate the conversation on three dimensions:

| Dimension | Question | Weight |
|-----------|----------|--------|
| **Task alignment** | Is the agent doing what the user explicitly asked for, even if the tools and outputs are not typical coding actions (tests, builds, file writes)? | 50% |
| **Outcome momentum** | Is the agent's work product growing or converging toward a solution? Are intermediate results narrowing the problem space? | 35% |
| **Fresh exploration** | Is each turn exploring new ground rather than revisiting known territory? Is the agent systematically probing rather than looping? | 15% |

Each dimension is scored 1–5. The aggregate is a weighted average.

### Response Format

```json
{
  "task_alignment": 3,
  "outcome_momentum": 4,
  "fresh_exploration": 2,
  "overall": 3.0,
  "meaningful_progress": true,
  "reasoning": "Agent is systematically profiling each slow query with EXPLAIN ANALYZE..."
}
```

`meaningful_progress` is true when the weighted average ≥ 3.0. The `overall` field in the model response is ignored — the server recomputes the weighted average to prevent gaming.

### Action on Result

| Result | Action |
|--------|--------|
| `meaningful_progress == true` | Reset `consecutiveNoProgress` to 0, reset all L1/L2 flags and `forkDispatched`, set level back to `ProgressLevelNone`. Agent continues normally. Emit a `system` stream event with `subtype: "progress_eval"` and the judge result. |
| `meaningful_progress == false` | Fall through to the existing L1-second flow: `CheckAndInject` proceeds in the same turn, injecting the L1 second reminder immediately. The agent then has 5 more turns before L2 triggers. |

### Fork Implementation

The fork follows the same pattern as the complexity evaluation fork, with a channel-based handoff to eliminate data races:

- Copies the full conversation history and appends the judge prompt as a final user message, preserving the shared prefix for prompt cache hits
- Uses a separate API client with `max_tokens` overridden to 200
- Runs with a 30-second timeout via `context.WithTimeout`
- Does not block the main loop — the fork runs in a goroutine and sends the result to the main loop via a buffered channel (`forkResultCh`, capacity 1, non-blocking send with drop-if-full)
- The main loop drains the channel via `DrainForkResult()` before each `CheckAndInject()` call, ensuring the result is applied on the main goroutine
- If the fork fails (timeout, API error, invalid response), the goroutine exits silently and the existing L1-second flow proceeds (safe default: assume no progress)

### Invariants

- The judge fork fires at most once per L1-second trigger (guarded by `ShouldForkProgressEval` which checks `consecutiveNoProgress >= l1SecondThreshold && !l1SecondFired && !forkDispatched`)
- `forkDispatched` resets on any progress signal (score > 0 in `RecordTurn`), ensuring a fresh fork opportunity on each new no-progress streak
- A positive judge result always resets the counter, regardless of the heuristic score
- A negative judge result does not alter the existing L1-second → L2 escalation path; `CheckAndInject` proceeds in the same turn
- Fork failures (network, timeout, parse error) never prevent escalation — safe default is "no progress"

## Integration Points

- **Pre-API check:** Before compaction and normalization, before each API call. Dispatches the L1-second fork-judge if `ShouldForkProgressEval()` returns true, drains any pending fork result via `DrainForkResult()`, then calls `CheckAndInject()` to inject L1/L2 reminders or trigger L3 termination.
- **Post-tool scoring:** After `executeAndProcessTools`. Calls `RecordTurn()` to score the turn and update escalation state, then `SetProgressLevel()` to sync the level to the max_tokens controller.
- **Fork-judge callback:** The fork goroutine sends the result via `forkResultCh` (non-blocking send). The main loop drains it via `DrainForkResult()` before `CheckAndInject`, calling `ApplyProgressEvalResult` on the main goroutine — eliminating data races.
- **max_tokens coupling:** L1 caps at 8192, L2 caps at 4096, L3 caps at 2048 (via `dynamic-max-tokens`), all with a floor of 2048.

## Invariants

- System prompt is never modified. All injections are virtual user messages.
- L1/L2 reminder text is structured but concise — L1 includes a self-assessment framework, L2 demands best-effort delivery.
- All counters reset on progress (score > 0), including `consecutiveNoProgress`, `level`, `l1FirstFired`, `l1SecondFired`, `l2Fired`, `l2TurnCount`, and `slidingL1Fired`.
- All counters also reset on a positive L1-second fork-judge result (`meaningful_progress == true`), regardless of the heuristic score.
- L3 is irreversible once triggered.
- L2 allows at most 5 extra API turns (tracked by `l2TurnCount`, incremented once per `CheckAndInject` call) before escalating to L3.
- Sliding window L1 trigger is guarded by `slidingL1Fired` to prevent repeated firing; resets on positive score.
