---
title: Query Engine Lifecycle
slug: query-engine
priority: P1
status: draft
spec: partial
code: done
package: internal/agent
gaps: []
depends_on:
  - session-persistence
  - agent-loop
  - cross-turn-state
  - cost-tracking
  - context-compaction
---
# Query Engine Lifecycle

## Overview

QueryEngine orchestrates headless query lifecycle: persist user input, run agent loop, restore session state, enforce turn/budget limits.

## Lifecycle

```
SubmitMessage(ctx, prompt) (string, error)
    → persist user message to transcript (before API)
    → runLoop (maxIterations bound, 0 = unlimited)
        → drain task completions (inject synthetic tool_results for background tasks)
        → check compaction thresholds (auto-compact with circuit breaker)
        → stream API response (with thinking token emission)
        → accumulate cost, build assistant message
        → handle stop reason (end_turn / tool_use / max_tokens / stop_seq)
        → execute tools, process results
        → sync skills, check session memory
    → flush transcript + cost state
    → yield SDK messages (stream-json)
```

## Persist Before API

Record user message to transcript **before** first API call of turn.

Survives process kill mid-request; resume shows user prompt even if assistant never responded.

## readFileState

- Constructor accepts `readFileCache` parameter.
- `seedReadFileCacheFromTranscript` seeds the cache on resume from transcript data.
- `WireTools` injects the cache into Read/Write/Edit/NotebookEdit tools.

## Cross-Turn State

Carried across turns in engine instance (see [`cross-turn-state.md`](./cross-turn-state.md)):

- `PermissionDenials` — security-gate denial cache
- `DiscoveredSkillNames` — skill names discovered via path activation
- `ActiveSkills` — currently active skill definitions
- `CachedSystemPrompt` — system prompt cached for resume comparison

## Limits

| Option | Behavior |
|--------|----------|
| `maxTurns` | Stop loop; emit `error_max_turns` result |
| `maxBudgetUsd` | Stop before API when cost exceeded |
| `maxIterations` | Maximum raw loop iterations (0 = unlimited); bounds API calls when set |
| `jsonSchema` | Structured output; requires synthetic output tool, validated at creation and invocation |

## Structured Output

Requires JSON schema param plus StructuredOutput tool in pool. Validation happens at tool creation (deny rules check) and at end-of-turn (`IsEmitted()` check).

## Thinking Tokens

The engine implements debounced thinking token emission (100ms) via `emitThinkingTokens`/`emitThinkingTokensFinal`. Per-block state tracking with delta calculation emits thinking progress as stream events.

## Secret Redaction

A `SecretRedactor` is applied to text output, tool result content, and recovered at end-of-turn. Prevents sensitive data leakage in stream-json output.

## Context Compaction

Integrated via `CompactConfig` and a circuit breaker (`compactFailCount`, max `MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES`). Token estimation checks thresholds each iteration; auto-compaction triggers when exceeded. Post-compaction re-injects active skills. See [`context-compaction.md`](./context-compaction.md).

## Session Memory

After each turn, `sessionMemory.CheckThreshold()` triggers `Init()` or `Update()` as needed.

## Memory Extraction

`initMemoryExtractor` initializes the extractor; `CheckAndExtract` is called in `finalizeAsEndTurn` and on stop-seq.

## Resume Change Detection

`detectResumeChanges` compares the frozen system prompt with the current environment (date, CWD, skills) and injects reminders for any detected changes.

## Task Completion Injection

`drainTaskCompletions` injects synthetic `tool_results` for completed background tasks before each API iteration.

## Web Search Results

Web search results from the API are processed: errors are surfaced as tool results, and results are emitted as user events in stream-json.

## Error Handling Categories

| Error | Behavior |
|-------|----------|
| ModelNotFound | Re-entry via non-streaming fallback |
| Quota/Payment | Fast-fail, stop loop |
| Content Filter | Fast-fail, stop loop |

## Skills and Plugins

Loaded per turn; merge coordinator userContext when enabled.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Abort mid-turn | Synthetic "Tool execution interrupted" tool_results; partial transcript persisted |
| Resume same engine | Rehydrate from transcript file |
| maxTurns = 1 | Single API iteration max |
| maxIterations = 0 | Unlimited loop iterations (default) |
| maxIterations = 5 | Stop after 5 raw loop iterations |
| Structured output invalid schema | Error at tool registration |

## File structure

`internal/agent/` splits the engine across five files:

| File | Responsibility |
|------|---------------|
| `engine.go` | Constructor, WireTools, SetMaxTurns, SetMaxBudgetUsd, initMemoryExtractor, compaction counters, persistCompactBoundary, detectResumeChanges, syncActiveSkills, seedReadFileCacheFromTranscript |
| `engine_loop.go` | `SubmitMessage`, `runLoop` (main loop with inline compaction, task injection, streaming, cost accumulation, session memory checks) |
| `engine_stream.go` | emitConsolidatedAssistant, finalizeAsEndTurn, thinking token emission (emitThinkingTokens/emitThinkingTokensFinal), TurnCount, Model, Drain, drainTaskCompletions |
| `engine_results.go` | executeAndProcessTools, handleStreamError, buildToolResultUserMsg — tool execution, result NDJSON emission, streaming error handling |
| `engine_stopreasons.go` | handleStopReason — stop reason switch for end_turn, tool_use, max_tokens, stop_seq, default; memory extraction calls |

## Acceptance Criteria

- **AC1:** User message on disk before API call starts.
- **AC2:** readFileState seeded from transcript on resume, injected into tools via WireTools.
- **AC3:** maxTurns enforced with distinct error subtype.
- **AC4:** maxBudgetUsd stops before next API call.
- **AC5:** Structured output requires schema + output tool.
