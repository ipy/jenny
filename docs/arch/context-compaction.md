---
title: Context Compaction
slug: context-compaction
priority: P2
status: done
spec: complete
code: done
package: internal/agent
gaps:
  []
depends_on:
  - message-normalization
  - query-engine
---
# Context Compaction

## Overview

Automatic and manual compaction summarizes older turns to fit within the model context window. Inserts a compact boundary and rebuilds a shorter message chain.

## Model Parameters

Thresholds are derived from per-model parameters via `api.ModelParams(model)`:

| Model | Context Window | Max Output Tokens |
|-------|---------------|-------------------|
| `deepseek-v4-*` | 1,000,000 | 384,000 |
| `claude-opus-4-*` | 200,000 | 128,000 |
| `claude-sonnet-4-*` / `claude-haiku-4-*` | 200,000 | 64,000 |
| Default (unknown models) | 200,000 | 16,384 |

`newCompactConfigForModel(model, streamCfg)` looks up actual values; `streamCfg` fields carry `DisableCompact`, `DisableAutoCompact`, and `EnableSessionMemory` (populated by koanf from `JENNY_DISABLE_*` / `JENNY_ENABLE_SESSION_MEMORY` env vars and matching CLI flags). `AUTO_COMPACT_WINDOW` env (non-`JENNY_*`) still overrides context window if set.

## Threshold Math

Threshold strategy depends on model context window size:

| Context Window | Threshold |
|---|---|
| `AUTO_COMPACT_WINDOW` set | `effectiveContextWindow - autoCompactBuffer` (original formula, user override) |
| ≤ 256K | `effectiveContextWindow - autoCompactBuffer` (original formula) |
| > 256K, ≤ 512K | 50% of context window |
| > 512K | Fixed 512K cap |

Where:

```
effectiveContextWindow = modelContextWindow - modelMaxOutputTokens
autoCompactBuffer      = max(modelMaxOutputTokens + 5_000, 13_000)
                        Capped at 50% of effectiveContextWindow (for tier 1 and user override only)
warningThreshold       = autoCompactThreshold - WARNING_BUFFER_TOKENS
blockingLimit          = effectiveContextWindow - BLOCKING_BUFFER_TOKENS  (when auto-compact off)
```

When `AUTO_COMPACT_WINDOW` is explicitly set, the user has taken manual control of the context window size, so the original formula is always used regardless of window size. The tiered approach only applies when using the model's default context window.

## Compaction Target (Hysteresis)

Auto-compact uses two token levels: a **trigger** (`autoCompactThreshold`, unchanged) and a **target** (`compactionTarget`) that post-compaction size should fall below. The gap between trigger and target prevents a compact storm — where compaction fires, the compressed chain is still near the trigger, and the next turn immediately compacts again.

Target strategy mirrors the trigger tiers:

| Context Window | Trigger (unchanged) | Target (new) | Gap |
|---|---|---|---|
| `AUTO_COMPACT_WINDOW` set | `effectiveContextWindow - autoCompactBuffer` | 60% of `effectiveContextWindow` | varies |
| ≤ 256K | `effectiveContextWindow - autoCompactBuffer` | 60% of `effectiveContextWindow` | ~20% |
| > 256K, ≤ 512K | 50% of context window | 35% of context window | 15% |
| > 512K | 512K (fixed cap) | 360K | ~30% |

Rules:

- Target is always strictly less than trigger when trigger > 0.
- When trigger is 0, target is 0.
- After compaction, if estimated post-compact tokens still exceed target, a warning is logged; compaction is not retried in the same turn.

## Trigger

Auto-compact when estimated tokens ≥ `autoCompactThreshold`.

**Disabled when:** `JENNY_DISABLE_COMPACT`, `JENNY_DISABLE_AUTO_COMPACT`, user setting off, or `querySource` is `compact` / `session_memory`. See [koanf-config.md](./koanf-config.md) for the full env-var/CLI-flag surface.

## Summary Reserve

Compaction summary call may consume up to **20K output tokens** (p99.99 summaries ≈ 17.4K).

## Circuit Breaker

Track `consecutiveFailures` per session.

- Failure (non-user-abort): increment.
- Success: reset to 0.
- After **3** failures (`MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES`): skip all further auto-compact for session.

The fail count is persisted to the transcript as `state` entries (`persistCompactFailCount`) so the circuit breaker state survives session resumes via `-r`.

## Execution

Three-step strategy (first success wins):

1. **In-session compaction** (`inSessionCompact`): appends a summary instruction to the existing message chain, reusing the cached system prompt + tools prefix. A pre-flight check skips this step if estimated message tokens exceed `effectiveContextWindow - wireOverhead - SUMMARY_MAX_TOKENS`, where `wireOverhead` is the precise system-prompt + tool-definition + fixed-overhead estimate (falling back to `MIN_SAFETY_OVERHEAD` when unavailable).
2. **Session-memory compaction** (when enabled): currently a stub — returns `"session memory compaction not implemented"`.
3. **Fork summary agent**: `querySource=compact`, tools disabled, max 1 turn. Strips images/documents from summarizer input (replace with `[image]` / `[document]` markers). On prompt-too-long during summarize: retry up to 3× dropping oldest API-round groups from head.

After successful compaction, active skills are re-injected as a `<system-reminder>` virtual user message to ensure skill activations survive compaction.

Post-compact chain order: `boundaryMarker → summaryMessages → tailMessages → attachments → hookResults`.

### Token-Based Tail Selection

The number of preserved tail messages is no longer fixed. Instead, a token budget drives selection:

```
tailTokenBudget = compactionTarget - estimatedTokens(boundaryMarker + summary)
```

Walk backwards from the end of the message chain, accumulating uncalibrated token estimates. Stop when adding the next message would exceed `tailTokenBudget`.

Constraints:

- **Minimum:** always keep at least 2 messages (current user prompt + preceding assistant).
- **Maximum:** cap at 30 messages even when budget allows (prevents degenerate cases).
- **Tool pairing:** the cut point never splits a `tool_use` / `tool_result` pair. If the boundary falls between an assistant with `tool_use` and its user with `tool_results`, the boundary moves earlier to include the full pair.

The uncalibrated charset-aware estimator (not the EWMA-calibrated estimator) is used for relative sizing within the tail walk.

`preserved_segment` in boundary metadata records the actual number of tail messages kept.

### Post-Compact Tail Pruning

After tail selection, a light pruning pass trims oversized tool results within the tail on the **compacted chain copy** (the persistent transcript is untouched):

- Scope: tool results in tail messages belonging to API rounds **older than the most recent 2**.
- Threshold: content exceeding 4 KB (4096 characters).
- Error tool results (`is_error: true`) are kept intact.
- Replacement format:

```
[Trimmed: original N chars. First 1024 chars shown, last 512 chars shown. Re-run the tool if the full data is needed.]
<first 1024 characters>
...
<last 512 characters>
```

## Persistence & Session Resume

To ensure compaction survives session resumes (`-r`), compaction boundaries are persisted to the transcript.

### Transcript Entry Schema
Compaction boundaries are recorded as `system` messages with `subtype: "compact_boundary"`:
```json
{
  "type": "system",
  "content": "",
  "subtype": "compact_boundary",
  "compact_metadata": {
    "trigger": "auto",
    "pre_tokens": 125000,
    "preserved_segment": 5,
    "summary": "<compacted summary text>"
  }
}
```

### Resume Filtering
When resuming a session, `LoadPostBoundaryMessages` scans the transcript for the *latest* `compact_boundary`. Only entries appearing after this boundary are sent to the API. This prevents re-sending uncompacted history and wasting tokens.

### Acceptance Criteria (Persistence)
- **AC8:** Compaction boundary persisted to transcript as `system`/`compact_boundary` entry.
- **AC9:** `LoadPostBoundaryMessages` returns only entries after the last boundary.
- **AC10:** Resume after compaction sends only post-boundary messages to API.
- **AC11:** Boundary metadata includes `trigger`, `pre_tokens`, `preserved_segment`, and `summary`.

## Post-Compact Normalization

Before next API call:

1. `filterOrphanedThinking` — remove thinking-only messages with no subsequent content.
2. `stripTrailingThinking` — strip trailing thinking blocks from all assistant messages.
3. `ensureNonEmptyAssistant` — insert `[Tool use interrupted]` if stripping left an empty assistant.
4. `filterWhitespaceOnly` — remove whitespace-only assistant messages.
5. `ensureToolResultPairing` — synthesize missing tool_result blocks.

## Warning and Hard Block

- Warning flags when tokens ≥ threshold − 20K.
- With auto-compact **off**: block API calls at `effectiveWindow - 3K` with prompt-too-long error (except compact/session_memory sources).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Re-compaction same chain | Track turnCounter for telemetry |
| Partial compact (from vs up_to) | Different cache invalidation |
| Task budget | Subtract pre-compact context from remaining budget |
| Blocking check after compact | Skip stale usage check once |

## Token Estimation

A single, unified estimator produces token counts for every consumer in the engine (compaction thresholds, thinking-token streaming events, Read-tool truncation guards). It is the only source of token counts in the system; all ad-hoc character-to-token heuristics in individual subsystems are replaced.

### Base Ratios (charset-aware)

- ASCII/Latin bytes: ~4 chars per token.
- Multi-byte (CJK, emoji, etc.): ~1.5 chars per token.

### Per-Model Calibration

After each API call, the estimator receives the actual `usage.input_tokens` and `usage.output_tokens` returned by the model, along with the character count of the corresponding text. It maintains a per-model exponentially-weighted moving average (α=0.2) of the ratio between actual and predicted tokens, and applies this factor to future estimates.

Input and output correction factors are tracked independently because prompt and completion have different character compositions. When no calibration data exists for a model, the raw base ratios are used.

Counts all message fields: `Content`, `Thinking`, `Signature`, `ToolUse` (name + input), and `ToolResults` (content + 50-char overhead).

Target accuracy: within 10% for English, within 30% for CJK-heavy content (without calibration). With 3-5 turns of calibration data, accuracy improves to within 5% for all content types.

### Wire-Equivalent Token Estimation

Local threshold checks (auto-compact trigger, warning, hard block) must reflect the **full API request payload**, not message content alone. A complete request includes:

| Component | Typical size | Included in wire estimate |
|-----------|-------------|---------------------------|
| Message chain | Variable | Yes (existing message estimation) |
| System prompt blocks | 3–5K tokens | Yes |
| Tool definition schemas | 5–15K tokens (depends on tool count) | Yes |
| Fixed request overhead | ~200 tokens (structure, cache markers, thinking params) | Yes |

**Wire-equivalent total** = message tokens + system prompt tokens + tool definition tokens + fixed overhead (~200).

#### Threshold Integration

Auto-compact, warning, and blocking checks compare estimated tokens against thresholds using the wire-equivalent total. This causes thresholds to fire earlier than message-only estimation — the intended behavior, since the API rejects requests based on the full payload.

#### Tool Definition Caching

Tool definitions are stable for the duration of a session. The tool-definition token count is computed once per session: tool params are serialized to JSON, token-estimated, and cached keyed by a fingerprint (hash of sorted tool names). Subsequent turns reuse the cached value without re-serializing.

#### Compaction Pre-Flight

The in-session compaction pre-flight check reserves space for non-message components using a precise wire overhead estimate (system prompt + tool definitions + fixed overhead) rather than the flat `MIN_SAFETY_OVERHEAD` (30,000) constant. `MIN_SAFETY_OVERHEAD` remains as a fallback when the precise overhead cannot be computed (zero result).

#### EWMA Calibration

API `usage.input_tokens` already includes system prompt and tool definitions. After each API response, the estimator calibrates against the **wire-equivalent prediction** (not message tokens alone) so the per-model correction factor accounts for systematic bias across the full request payload.

## Acceptance Criteria

- **AC1:** Auto-compact at effectiveWindow − buffer.
- **AC2:** Circuit breaker after 3 failures.
- **AC3:** Hard block at effectiveWindow − 3K when auto off.
- **AC4:** Post-compact payload passes tool/thinking pairing rules.
- **AC5:** compact/session_memory sources never hard-blocked pre-API.
- **AC6:** Thresholds derived from actual model context window and max output tokens via `api.ModelParams`.
- **AC7:** `buildCompactedChain` does not split tool_use/tool_result pairs at boundary.
- **AC12:** `compactionTarget` is strictly less than `autoCompactThreshold` for all positive thresholds, following the tier table above.
- **AC13:** Tail selection keeps between 2 and 30 messages, bounded by `tailTokenBudget` derived from `compactionTarget`.
- **AC14:** Post-compact tail pruning trims tool results older than the most recent 2 API rounds when content exceeds 4 KB; error tool results are preserved.
- **AC15:** Post-compaction estimated tokens exceeding `compactionTarget` emit a warning without failing the turn.

## Error Reporting: `stop_reason: max_tokens` {#error-reporting-stop_reason-max_tokens}

When the streaming API returns `stop_reason: "max_tokens"`, the engine emits a structured `result` event with `subtype: "error_max_tokens"` to distinguish between two failure categories:

### Categories

| Category | Condition | Fields Emitted |
|----------|-----------|----------------|
| `output_cap_hit` | `output_tokens >= modelMaxOutputTokens` | `category`, `model`, `output_tokens`, `max_output_tokens` |
| `context_exhausted` | `output_tokens < modelMaxOutputTokens` (request limited/rejected) | `category`, `model`, `output_tokens`, `max_output_tokens`, `input_tokens`, `threshold` |

### Structured `result` Event Schema

```json
{
  "type": "result",
  "subtype": "error_max_tokens",
  "result": "max tokens reached: <category>",
  "model": "<model_name>",
  "usage": {
    "input_tokens": <int>,
    "output_tokens": <int>,
    "cache_read_input_tokens": <int>,
    "cache_creation_input_tokens": <int>
  },
  "error_max_tokens": {
    "category": "<output_cap_hit|context_exhausted>",
    "output_tokens": <int>,        // only for output_cap_hit
    "max_output_tokens": <int>,   // only for output_cap_hit
    "input_tokens": <int>,        // only for context_exhausted
    "threshold": <int>             // only for context_exhausted (autoCompactThreshold)
  },
  "stop_reason": "max_tokens",
  "duration_ms": <int>,
  "total_cost_usd": <float>,
  "total_cost_cny": <float>
}
```

### Category Selection Rule

The categorizer applies this logic:

1. **output_cap_hit** — The streaming response completed successfully with `stop_reason: "max_tokens"` and `output_tokens >= modelMaxOutputTokens`. The model hit its per-response output cap mid-generation.

2. **context_exhausted** — The streaming response completed with `stop_reason: "max_tokens"` but `output_tokens < modelMaxOutputTokens`. The request was rejected or severely limited before the model could generate significant output. This indicates the input context was too large for the model to generate meaningful response within its output cap.

### Model-Specific Max Output Tokens

| Model | Max Output Tokens |
|-------|-------------------|
| `deepseek-v4-*` | 384,000 |
| `claude-opus-4-*` | 128,000 |
| `claude-sonnet-4-*` / `claude-haiku-4-*` | 64,000 |
| Default (unknown models) | 16,384 |

### Backward Compatibility

The new `error_max_tokens` subtype is **additive** to the stream-json output. Existing event subtypes (`success`, `error`, etc.) are unchanged. Consumers should ignore unknown subtypes to maintain forward compatibility with future event types.
