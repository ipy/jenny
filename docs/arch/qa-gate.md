---
title: QA Gate
slug: qa-gate
priority: P2
status: partial
spec: complete
code: partial
package: internal/agent
depends_on:
  - side-query
  - dynamic-max-tokens
  - subagent-types
  - stream-json
---
# QA Gate

## Overview

The QA gate intercepts `end_turn` for complex or long-running tasks and spawns a fresh verification agent to evaluate deliverables against recorded acceptance criteria. The evaluation is synchronous within the same `SubmitMessage` call — the SDK consumer sees no intermediate QA cycle.

## Acceptance Criteria Extraction

Acceptance criteria and constraints are extracted by extending the existing complexity evaluation side query (slug: `dynamic-max-tokens`). When the complexity fork fires (after the 3rd tool result), the prompt also requests:

- `accept_criteria`: A list of measurable acceptance criteria inferred from the user's initial request.
- `constraints`: A list of constraints or restrictions stated or implied by the user.

These fields are added to the same JSON response alongside the four complexity dimensions. The `forkMaxTokens` budget is increased to accommodate the longer output.

**Fallback:** When the complexity eval has not yet fired by the time QA triggers (e.g., tasks completing in fewer than 3 tool turns), the same `complexityEvalPrompt` is executed synchronously via SideQuery. One API call produces both the complexity dimensions and the acceptance criteria. The result is stored in `MaxTokensController`, so the dynamic token budget also benefits from the late evaluation. No separate AC-only query is ever sent.

## Trigger Conditions

`shouldTriggerQA` returns true when **all** of the following hold:

| Condition | Rationale |
|-----------|-----------|
| Stream-json mode is enabled | QA events are only meaningful in structured output mode |
| Not a fork child (`IsForkChild == false`) | Prevents QA inside subagents or verification agents |
| Not a resumed session (`IsResume == false`) | Resumed sessions already completed prior QA evaluation |
| QA retry count < max retries (default: 2) | Bounded retry budget |
| At least one complexity signal is high | See below |

**Complexity signals** — at least one must be true:

- `ComplexityResult` exists and `OverallComplexity() >= 3.0`
- Turn count >= 10 (long session regardless of complexity score)
- Auto-compact has occurred during this session (session was long enough to require compaction)

## Verification Agent

A fresh, one-shot agent with restricted capabilities:

| Parameter | Value |
|-----------|-------|
| Tools | Read, Glob, Grep, Bash |
| System prompt | QA evaluator instructions (read-only mandate) |
| Timeout | 60 seconds |
| Max output tokens | 4096 |
| Max turns | 5 |
| IsForkChild | true |

The verification agent receives a prompt containing:

1. The extracted acceptance criteria and constraints
2. The working directory
3. A summary of modifications (files written/edited, commands run) extracted from the main conversation
4. Instructions to return a structured JSON verdict

The agent may use Read/Glob/Grep/Bash to independently inspect the filesystem and verify deliverables. The system prompt instructs it to use Bash only for read-only commands (listing, checking, testing — never modifying).

### Verdict Format

The verification agent must return a JSON object:

```json
{
  "verdict": "PASS" | "FAIL",
  "reasoning": "Brief explanation of the evaluation",
  "gaps": ["Unmet criterion 1", "Unmet criterion 2"],
  "suggestions": ["Fix suggestion 1", "Fix suggestion 2"]
}
```

## Flow

```
end_turn fires
    │
    ▼
shouldTriggerQA()
    ├── false → normal delivery (emitSuccessResult, memory extraction)
    │
    ▼ true
extractAcceptCriteria()
    │  (from ComplexityResult if available, otherwise synchronous complexityEval)
    │
    ▼
runQAGate()
    │  spawn verification agent with AC + file summary
    │  timeout: 60s, max turns: 5
    │
    ▼
parse verdict
    │
    ├── PASS → emit system/qa_eval event → emitSuccessResult → memory extraction → return
    │
    └── FAIL →
        ├── retries exhausted → emit system/qa_eval event → emitSuccessResult (with QA metadata) → return
        │
        └── retries remain →
            emit system/qa_eval event
            inject QA feedback as <system-reminder> virtual user message
            append current tool results (if any)
            return shouldContinue=true (re-enters runLoop)
```

## Retry Feedback Injection

When the verdict is FAIL and retries remain, a virtual user message is injected:

```
<system-reminder>
QA verification FAILED (attempt N/M).

Unmet acceptance criteria:
- [gap 1]
- [gap 2]

Suggested fixes:
- [suggestion 1]
- [suggestion 2]

Address the gaps above and complete the task. Do NOT explain what you will do — just do it.
</system-reminder>
```

This message is virtual (`IsVirtual: true`) and is not persisted to the transcript.

## Stream Events

### QA Evaluation Event

Emitted after each QA evaluation (both PASS and FAIL):

```json
{
  "type": "system",
  "subtype": "qa_eval",
  "result": "{\"verdict\":\"PASS\",\"reasoning\":\"...\",\"gaps\":[],\"retry_count\":0,\"max_retries\":2}",
  "uuid": "...",
  "totalCostUsd": 0.05,
  "modelUsage": {...}
}
```

## Usage Tracking

All API calls from QA gate operations are accumulated in the session's `CostState`:

- Synchronous complexity eval fallback (if needed) via `SideQuery.Execute` → `AccumulateUsage`
- Verification subagent via `RunStream` → child engine `AccumulateUsage`, then cost merge back to parent

No separate acceptance-criteria-only query is ever sent. When the async complexity eval has already fired, the QA gate reads from the cached `ComplexityResult` at zero additional cost.

## Invariants

- QA never triggers inside fork children, subagents, or resumed sessions
- QA retries are bounded by `qaMaxRetries` (default 2)
- The `result` event is emitted exactly once per `SubmitMessage`: after QA passes or retries are exhausted
- Memory extraction runs only on final delivery, not on QA retry iterations
- The QA cycle is invisible to the SDK user — no intermediate `result` events are emitted
- Acceptance criteria are extracted at most once per session (cached after first extraction)
- The verification agent cannot modify files (Write/Edit tools excluded, Bash instructed as read-only)
- Cost from QA operations is included in the terminal `result` event's usage fields

## Acceptance Criteria

- The QA gate triggers only for complex or long-running tasks meeting the threshold conditions
- The verification agent independently inspects the filesystem to validate deliverables
- QA failure feedback is actionable and re-enters the main loop with preserved context
- The feature is transparent to SDK consumers (no protocol-breaking changes)
- Resume/continue of QA-restarted sessions is out of scope for initial implementation
