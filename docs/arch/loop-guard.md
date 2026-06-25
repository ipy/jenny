---
title: Loop Guard
slug: loop-guard
priority: P1
status: done
spec: complete
code: done
package: internal/agent
depends_on:
  - progress-monitor
---
# Loop Guard

## Overview

Loop Guard detects exact repetition patterns during agent sessions — repeated tool calls with identical arguments and repeated assistant text — and feeds strong negative signals into the Progress Monitor escalation pipeline. It does not maintain its own escalation levels; reminders are injected as virtual user messages using the same `<system-reminder>` pattern as the Progress Monitor.

## Tool Call Repetition

### Identity

Each tool invocation is keyed by `(tool name, canonical arguments)`. Arguments are canonicalized by JSON-serializing the input map with sorted keys and normalized whitespace in string values, then hashing. Key order differences (`{"a":1,"b":2}` vs `{"b":2,"a":1}`) produce the same identity.

### Cross-turn tracking

A bounded store (capacity ~100 unique identities) tracks how many times each `(tool, args)` pair has been requested across turns. When the store is full, the least-recently-used entry is evicted.

| Occurrences | Behavior |
|-------------|----------|
| 1–2 | Execute normally |
| 3+ (soft threshold) | Execute, but contribute a warning signal and suggested `<system-reminder>` text |
| 6+ (hard threshold) | Do not execute; synthesize an error tool result with a blocking message |

Each soft-threshold hit contributes a negative score adjustment to the turn. Each hard-threshold block contributes a stronger negative score adjustment and a distinct signal.

### No batch-internal deduplication

Every tool call in a batch is always executed. Even read-only tools may depend on side effects of other tools in the same batch (e.g., `Read` after `Bash` that modified the file), so deduplication is not applied.

### Progress-aware reset

When a write, edit, or notebook edit succeeds on a **new** file path (one not previously written in the session), the repetition counter for the tool call identity associated with that path is reset to zero. Rewrites to already-written paths do not reset counters.

## Assistant Text Repetition

A sliding window retains normalized hashes of the last 10 assistant text outputs. Normalization strips whitespace and lowercases text before hashing.

Two patterns are detected:

| Pattern | Definition | Score |
|---------|------------|-------|
| Direct repetition | Same normalized text appears 3+ times consecutively | −5 |
| Cyclic repetition | A subsequence of length 2–5 repeats 3+ full cycles consecutively at the end of the window | −5 |

Cycle detection operates on the hash window: for each candidate cycle length 2–5, the trailing segment is checked for at least three consecutive identical repetitions of the pattern. This avoids false positives from normal two-cycle workflows (e.g., read → edit → test → read → edit → test).

When either pattern is detected, Loop Guard contributes the negative score and descriptive signals to the turn score consumed by the Progress Monitor.

## Progress Monitor Integration

Loop Guard does not escalate independently. Its contributions merge into the existing turn scoring path:

- Assistant text repetition score and signals are included when the Progress Monitor records the turn.
- Tool repetition warnings and score adjustments from the pre-execution evaluation are included in the same turn record.
- Soft-threshold tool warnings are accumulated and injected as a single virtual `<system-reminder>` user message after tool results are appended.

When the Progress Monitor resets counters (positive turn score or positive fork-judge result), Loop Guard state is also cleared.

## Reminder Delivery

All Loop Guard reminders are virtual user messages appended to the message chain. They do not modify the system prompt or prior messages, preserving prompt cache stability.

## Memory Bounds

| Structure | Limit |
|-----------|-------|
| Unique tool call identities | ~100 (LRU eviction) |
| Assistant text hash window | 10 turns |
| Written file paths | ~200 (LRU eviction) |

## Invariants

- Loop Guard has no L1/L2/L3 levels; escalation is entirely driven by the Progress Monitor.
- Hard-threshold tool calls are never executed; the model receives a synthetic error tool result.
- Every tool call in a batch is always executed (no batch dedup).
- Reset clears tool call counts, the text hash window, and written-path tracking.
