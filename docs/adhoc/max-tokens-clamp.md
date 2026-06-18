---
title: max_tokens Configuration Mismatch and Silent Truncation
slug: max-tokens-clamp
status: proposed
date: 2026-06-18
updated: 2026-06-19
package: internal/api
gaps:
  - No pre-request validation of max_tokens against per-model limits
  - Anthropic provider default (32000) is below the actual model capability (64000–128000)
  - Streaming fallback hard-codes 64000 regardless of model capability
  - modelMaxOutputTokens table covers only two deepseek models with stale values (8192 vs actual 384000)
  - Truncation errors cannot distinguish user misconfiguration from legitimate output limits
---

# max_tokens Configuration Mismatch and Silent Truncation

## Problem

Jenny's `max_tokens` handling has three classes of defects that together produce a strictly worse experience than necessary:

1. **No request-side validation.** A caller (CLI flag, koanf config, sticky router) that sets `max_tokens` greater than the active model's actual limit will not learn this until the request either silently truncates (`stop_reason: "max_tokens"`) or returns an HTTP 400 from the upstream API. The `MaxTokensError.MaxOutputTokens` field returned to the caller is itself derived from a broken lookup, so it cannot be used to diagnose the problem.

2. **Internal inconsistency between default sources of truth.** The Anthropic provider's request builder falls back to `32000` when no override is set, while its own `modelMaxOutputTokens` function returns `20000` as the default. Both values are stale: current Claude models support 64000–128000 output tokens. The result is that **every default-configured Anthropic request underutilizes the model's actual capability** — a less dangerous failure mode than over-requesting, but still a silent misconfiguration that prevents callers from obtaining full-length outputs.

3. **Hard-coded values that bypass the capability table.** The streaming-to-non-streaming fallback in `engine_loop.go` writes `64000` directly to the client, ignoring both the caller's intent and the model capability. For Claude Opus models (128000 limit), this artificially restricts recovery output. For DeepSeek V4 models (384000 limit), the restriction is severe. The capability table itself only covers two `deepseek-v4-*` model strings with **stale values** (8192 vs. actual 384000); every other model falls into a single default bucket.

The net effect is that `max_tokens` is, in practice, a "best effort, may break" setting rather than a hard contract. This document specifies a single change that makes the system respect the model limit as a hard invariant.

## Root Cause

`SetMaxTokensOverride` is a thin setter with no contract on its argument. It stores the value verbatim and forwards it to the provider, which forwards it to the API. There is no layer that asks "is this value within the capability of the current model?" because no layer owns that question — the capability lookup is a free function in one provider file, the defaults are scattered across four provider constructors, and the fallback path doesn't even consult the table.

The additional complexity: when the external model registry (`external-model-registry.md`) lands, the capability table becomes a three-layer lookup (registry → user override → bundled defaults). The resolution function must be ready for this; the registry is a better source than hand-maintained constants, but the bundled table must still exist as the final fallback.

The fix is to centralize the resolution of "what `max_tokens` value do I actually send" into a single function that:

- Consults the per-model capability table (bundled, and in future: registry + user override)
- Applies the caller's override only when it does not exceed the capability
- Falls back to a conservative default when nothing is configured
- Emits a structured warning when it clamps a caller-supplied value
- Is used by every code path that builds an API request — streaming, non-streaming, and fallback

## Proposed Design

### Invariant

> Every API request issued by `internal/api` carries a `max_tokens` value `≤ ModelMaxOutputTokens(model)`. Violations of this invariant are prevented at the request site, not detected after the fact.

The invariant must hold for all three request sites: streaming, non-streaming, and the streaming-fallback path.

### Resolution function

A single function, `resolveMaxTokens(model string, override int) int`, lives in `internal/api` and is the only place that consults the capability table. Signature:

```go
// resolveMaxTokens returns the max_tokens value to send for the given model.
// It is the single resolution point for every request site (streaming,
// non-streaming, fallback). The override is the caller's requested value
// (0 means "use default"). The returned value is guaranteed to be within
// the model's capability.
func resolveMaxTokens(model string, override int) int
```

Resolution order:

1. If `override > 0` and `override <= capability` → return `override`.
2. If `override > 0` and `override > capability` → return `capability`, emit a warning.
3. If `override == 0` → return `capability` (no warning; this is the normal default).
4. If `override < 0` → return `capability`, emit a warning (treat as "unset").

This collapses the current "provider default fallback" (Anthropic: 32000, OpenAI/GenAI: 64000) and the "capability table default" (20000) into one authoritative number per model. The provider's local `maxTokens` field is removed; providers call `resolveMaxTokens(p.model, c.maxTokensOverride)` at request build time.

### Capability table

A new file `internal/api/model_caps.go` owns the per-model capability table. The table is data, not a switch statement, and is the single source of truth for all default values currently scattered across providers.

Initial coverage (verified June 2026):

| Model family | Max output tokens | Notes |
|---|---|---|
| `claude-opus-4-8`, `claude-opus-4-7`, `claude-opus-4-6` | 128000 | 1M context; 300K on Batch API |
| `claude-fable-5` | 128000 | 1M context |
| `claude-sonnet-4-6`, `claude-sonnet-4-5` | 64000 | |
| `claude-haiku-4-5` | 64000 | 200K context |
| `gpt-5`, `gpt-5-mini` | 128000 | 400K context |
| `gpt-5.5`, `gpt-5.5-mini`, `gpt-5.5-nano` | 128000 | 1.1M context (gpt-5.5); 400K (mini/nano) |
| `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano` | 128000 | |
| `gpt-5-nano` | 128000 | 400K context |
| `gpt-4.1`, `gpt-4.1-mini` | 33000 | 1M context |
| `gpt-4o`, `gpt-4o-mini` | 16384 | |
| `o3`, `o3-mini`, `o4-mini` | 100000 | reasoning tokens included in limit |
| `o3-pro` | 100000 | |
| `deepseek-v4-flash`, `deepseek-v4-pro` | 384000 | 1M context; legacy `deepseek-chat`/`deepseek-reasoner` route to v4-flash |
| `gemini-2.5-pro`, `gemini-2.5-flash` | 65536 | thinking tokens counted against output limit |
| *unknown* | 16384 | conservative fallback; emit warning |

The unknown-model fallback is intentionally conservative. An unknown model receiving a 128000-token request that the actual model only supports at 16000 is the exact failure mode this design exists to prevent. The fallback of 16384 is chosen because it is the lowest "common floor" among current-generation production models (matching GPT-4o). A warning at this layer is the appropriate signal: "we don't know your model's limit, so we picked something safe."

**Note on DeepSeek V4:** The existing `modelMaxOutputTokens` table lists 8192 for `deepseek-v4-flash` and `deepseek-v4-pro`. This is off by a factor of 47×. The actual limit is 384000. This is the single most impactful correction in this table.

### Warning channel

Clamp events emit a single structured log line at WARN level via the existing `slog` logger. Format:

```json
{
  "level": "WARN",
  "msg": "max_tokens clamped to model limit",
  "model": "gpt-4o",
  "requested": 64000,
  "applied": 16384,
  "reason": "override_exceeds_capability"
}
```

For unknown models, the reason is `unknown_model_capability_default`. In streaming mode, the warning is also surfaced as a `system` event in the stream-json protocol so headless operators can observe it. This is a single, narrow addition to the streaming protocol; it does not require a new event type — `system` events already exist and carry arbitrary key-value payloads.

### Fallback path

`engine_loop.go`'s `fallbackFn` is rewritten to use the same `resolveMaxTokens`:

```go
fallbackFn := func(fallbackCtx context.Context) (*api.Response, error) {
    e.client.SetMaxTokensOverride(resolveMaxTokens(model, e.callerMaxTokens))
    return e.client.SendMessage(fallbackCtx, messages, e.toolParams, nil, systemPrompt, "")
}
```

The hard-coded `64000` is removed. The fallback is no longer a special case; it is a third caller of `resolveMaxTokens`, no different from streaming or non-streaming.

### Provider changes

Each of the four providers (`anthropic`, `openai`, `openai_responses`, `genai`) deletes its `maxTokens` field and the local fallback in its request builder. Request construction calls `resolveMaxTokens(p.model, override)` directly. The `SetMaxTokensOverride` method on the provider interface is removed — `Client` stores the override and resolves at request time.

This is a deliberate inversion: the Client is now the policy owner, the provider is a thin transport. The provider knows the wire format, not the limit. The capability table is the only thing that knows the limit.

### Categorization accuracy

`categorizeMaxTokensError` continues to populate `MaxOutputTokens` from the capability table. With the table now comprehensive, this field is trustworthy: a caller receiving `MaxOutputTokens: 128000` for Claude Opus 4.8 can trust that 128000 is the actual model limit, not a placeholder.

A `CategoryConfigOversize` is **not** introduced. The argument for adding it is "let the caller distinguish misconfiguration from legitimate truncation." With the pre-request clamp in place, the misconfiguration case never produces a `MaxTokensError` — it produces a warning log and a successful request with a clamped value. The legitimate-truncation case is then the *only* case that surfaces as `CategoryOutputCapHit`, and the `MaxOutputTokens` field tells the caller exactly what ceiling the model has. No new category is needed; the information is already present.

## Rationale

**Why clamp on the request side, not retry-after-truncation?**

Truncation means the model produced output that was discarded. Retrying with a higher limit is a guess about what the model would have said. If the model genuinely needed 100k tokens to express a complete answer, the second request with 100k as the limit may still truncate. If the model could have been told to be more concise, the caller should have asked for that in the prompt, not in the limit. Pre-request clamp is deterministic; post-truncation retry is heuristic.

**Why consolidate defaults into the capability table?**

The current split — provider-local default (32000 for Anthropic, 64000 for OpenAI/GenAI) and capability-table default (20000) — is a bug factory. The magnitude of drift is severe: DeepSeek V4 models have a 384000-token capability while the table returns 8192, a 47× undercount. Every new model added to a provider constructor is a chance for the two to drift. With one table owning both, drift is impossible.

**Why warn instead of error?**

A misconfigured `max_tokens` is almost always a caller mistake, not a fatal condition. The right response is "I noticed, here's what I did, please fix your config" — not "request rejected." The warning is loud enough for monitoring to catch, the request still completes, and the caller can correct the configuration at their own pace.

**Why remove the provider's `maxTokens` field?**

It exists only to bridge `SetMaxTokensOverride` to the request builder. With `Client` resolving at request time, the field is dead state. Deleting it removes a class of "did the provider and client agree on the value?" bugs that are currently possible (see the `engine_loop.go:332` fallback that bypasses the provider's field entirely).

**Why is the streaming fallback a special concern?**

It is the only path that bypasses both the caller's intent and the capability table entirely. The hard-coded 64000 happens to be within bounds for Claude Sonnet 4.6 and Haiku 4.5 (both 64000), but it artificially caps Claude Opus models (128000) and severely restricts DeepSeek V4 (384000). For GPT-4o (16384), the value is 4× above the model's limit and will be rejected. The fallback is a recovery path; sending an invalid `max_tokens` defeats its purpose.

## Test Strategy

Unit tests for `resolveMaxTokens`:

- Returns the override when within capability
- Clamps to capability and emits warning when override exceeds
- Returns capability when override is 0
- Treats negative override as 0
- Returns conservative default with `unknown_model_capability_default` reason for unknown model

Unit tests for the capability table:

- Every model name listed in the provider constructors' known model sets appears in the table
- Unknown model returns the conservative default (16384)
- Table values are within sensible bounds (≥ 1024, ≤ 400000)
- DeepSeek V4 models return 384000 (regression against the stale 8192 value)

Integration tests via the existing mock API harness:

- A request with override > capability produces a log line with `clamped: true` and an API request with the clamped value
- The streaming fallback path produces the same clamped value as the primary streaming path
- An unknown model produces the conservative default and the corresponding warning

Regression coverage:

- The Anthropic default-config case (no override) sends a value that matches the model's actual capability (e.g. 128000 for Opus 4.8, 64000 for Sonnet 4.6)
- The streaming fallback no longer hard-codes 64000
- DeepSeek V4 models no longer report 8192 in `MaxTokensError.MaxOutputTokens`

## Out of Scope

- Per-request dynamic max_tokens (e.g. adjusting based on prior turn lengths). This is a separate optimization with different tradeoffs.
- Exposing `MaxOutputTokens` in the public stream-json event schema beyond the existing `error_max_tokens` event. Operators who need it can read the capability table.
- Adding new model entries beyond the initial coverage list. The table is data; adding entries is a one-line change without a spec change.
- Reasoning-token accounting for o-series models. The capability values listed assume max output tokens as documented; reasoning overhead is the provider's responsibility.

## Migration

This is an internal refactor with one externally visible change: callers may observe a `WARN` log line and a `system` stream event when their `max_tokens` exceeds the model limit. The behavior of the system when configured correctly is unchanged.

Order of work:

1. Add `internal/api/model_caps.go` with the table and `resolveMaxTokens`.
2. Add tests for the table and the resolution function.
3. Update `Client.SetMaxTokensOverride` to be a pure setter (no forwarding).
4. Update each provider's request builder to call `resolveMaxTokens` directly; delete the `maxTokens` field and `SetMaxTokensOverride` method.
5. Update `engine_loop.go` fallback to use `resolveMaxTokens`.
6. Update `categorizeMaxTokensError` to read from the new table.
7. Run the full test suite; confirm no regressions.
8. Once stable, fold the relevant paragraph into `docs/arch/provider-architecture.md` under a new "max_tokens resolution" section and delete this adhoc document.
