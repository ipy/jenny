---
title: Rate Limit Handling & Universal Error Classification
slug: rate-limit-handling
priority: P1
status: done
spec: partial
code: partial
package: internal/api, internal/api/router
gaps:
  - Retry-After header not yet wired from HTTP response into category backoff path
  - Max Output Token Recovery not implemented (doc describes planned behavior)
  - Background 529 suppression only implemented for Anthropic provider
  - openAIResponsesProvider streaming path does not populate ErrorInfo
depends_on:
  - anthropic-api-client
  - multi-provider-routing
---
# Rate Limit Handling & Universal Error Classification

## Overview

API client retries transient failures with exponential backoff. Every non-2xx HTTP response is classified into a semantic error category before any retry or routing decision. The category determines retryability, backoff strategy, and routing behavior — not the raw HTTP status code.

## Error Categories

Every API error is assigned one of the following categories. The classification replaces the previous per-provider keyword matching with a single, normalized semantic layer.

| Category | Meaning | Retryable |
|----------|---------|-----------|
| `unknown` | Unclassifiable error | Falls through to status-code rules |
| `auth` | Invalid or missing API key (401) | No |
| `permission` | Valid key, insufficient permissions (403) | No |
| `invalid_request` | Malformed request (400 default) | No |
| `context_exhausted` | Input too long for the model's context window (400/413/500) | Yes (returned to engine for compaction) |
| `rate_limit_rpm` | Requests-per-minute limit hit | Yes |
| `rate_limit_tpm` | Tokens-per-minute limit hit | Yes |
| `rate_limit_concurrency` | Concurrent request limit hit | Yes |
| `rate_limit_generic` | Rate limit with no subtype signal | Yes |
| `quota_exhausted` | Account quota or credit balance exhausted | No |
| `payment_required` | Payment or billing issue (402) | No |
| `content_filter` | Content policy violation or safety refusal | No |
| `server_overload` | Provider overloaded (503/529) | Yes (long backoff) |
| `server_error` | Generic server error (5xx) | Yes |
| `timeout` | Request timed out (504/408) | Yes |
| `cancelled` | Request cancelled by caller (499) | No |
| `model_not_found` | Model does not exist at provider (404) | No (triggers L3 model fallback) |
| `output_cap_hit` | Output truncated at model's max output token limit | No (returned to engine) |

## Classification Chain

Classification runs as a three-layer pipeline. Each layer returns `unknown` when it has no match, passing through to the next:

1. **Domestic provider classifier** — Parses provider-specific business codes from JSON response bodies (讯飞, 智谱, MiniMax, 阿里百炼). These providers embed semantic error codes inside HTTP 200/400 bodies that differ from standard HTTP semantics.
2. **International provider classifier** — Handles typed error metadata from providers with structured error schemas (OpenRouter `error_type`, AWS Bedrock `__type`, Fireworks dual-mode 429, Groq 498/413/499).
3. **Common classifier** — Universal keyword scan and status code mapping. Handles the majority of errors across all providers.

The active provider name determines which domestic/international classifier runs. All three layers share the same output type.

### Classification Priority (Common Layer)

1. HTTP 413 → `context_exhausted`
2. HTTP 402 → `payment_required`
3. HTTP 400/500/504 keyword scan — context keywords first, then content filter, then quota/payment
4. HTTP 429 disaggregation (see below)
5. HTTP 5xx mapping — 529/503→`server_overload`, 504→`timeout`, 498→`server_overload`, else→`server_error`
6. HTTP 401→`auth`, 403→`permission`, 499→`cancelled`, 404→`model_not_found`
7. HTTP 400 default→`invalid_request`

### 429 Disaggregation

HTTP 429 carries multiple distinct meanings. The classifier inspects the response body to assign a specific subcategory:

1. **`server_overload`** — message indicates overload, heavy load, capacity, or `Retry-After` > 30s
2. **`quota_exhausted`** — message indicates quota, balance, or limit exhaustion (not context)
3. **`rate_limit_rpm`** — message indicates per-second/per-minute request limits
4. **`rate_limit_tpm`** — message indicates token-per-minute limits
5. **`rate_limit_concurrency`** — message indicates concurrent request limits
6. **`rate_limit_generic`** — fallback when no subtype keyword matches

## Retryable Conditions

When an error carries a known category, retryability is determined by category. When the category is `unknown`, the system falls back to HTTP status code rules.

### Category-Based Retryability

| Retryable | Categories |
|-----------|-----------|
| Yes | `rate_limit_rpm`, `rate_limit_tpm`, `rate_limit_concurrency`, `rate_limit_generic`, `server_overload`, `server_error`, `timeout`, `context_exhausted` |
| No | `quota_exhausted`, `payment_required`, `content_filter`, `auth`, `permission`, `model_not_found`, `cancelled`, `invalid_request` |

### Status Code Fallback (when category is `unknown`)

| Condition | Retry |
|-----------|-------|
| HTTP 429 | Yes |
| HTTP 529 | Yes (foreground, capped) |
| HTTP 498 | Yes (Groq server overload) |
| HTTP 413 | Yes (context exhausted; engine handles compaction) |
| HTTP 408, 409 | Yes |
| HTTP 5xx | Yes |
| Connection errors | Yes (transient only: timeouts, temporary DNS; NOT connection refused, host unreachable) |
| `x-should-retry: false` header | No (overrides all other signals) |
| Non-retryable 4xx | No |

## Category-Aware Backoff

Each error category has its own backoff parameters. The sticky router selects the appropriate schedule based on the error's category.

| Category | Base Delay | Max Delay |
|----------|-----------|-----------|
| `rate_limit_rpm` | 2s | 15s |
| `rate_limit_tpm` | 5s | 30s |
| `rate_limit_concurrency` | 10s | 60s |
| `rate_limit_generic` | 1s | 32s |
| `server_overload` | 15s | 120s |
| `server_error` | 500ms | 32s |
| `timeout` | 2s | 30s |

Jitter: ±25% on computed delay. `Retry-After` header takes precedence when present.

### Default Backoff (status-code fallback)

- Base delay: 500ms
- Exponential cap: 32s
- Jitter: 25%
- Default max retries: 10 (programmatic override via `SetRetryConfig`)

## 529 Overload Cap

After 3 consecutive 529 errors, the request fails with a terminal error ("Repeated 529 Overloaded errors"). No automatic fallback to a different model.

## Foreground vs Background

Background query sources (classifiers, summaries, memory extraction) do **not** retry 529 errors (immediate terminal error). This prevents retry amplification on auxiliary calls.

Foreground sources (main agent loop) receive the full 529 retry budget.

**Note:** Background 529 suppression is currently only implemented for the Anthropic provider (`anthropicProvider.SetBackground`). Other providers always use the foreground retry path.

## Retry Context Preservation

Each retry passes unchanged:

- `model`
- `thinkingConfig`
- `maxTokensOverride`

## Max Output Token Recovery (Planned)

**Not yet implemented.** Planned behavior on max-output-tokens stop reason:

- Retry with increased `maxTokensOverride` (bounded).
- Cap recovery retries to avoid infinite loops (see agent loop).

## Engine-Level Error Recovery

### ErrorInfo Propagation in Streaming

All providers populate streaming error information with the fully resolved category, cleaned message, and status code for non-2xx HTTP responses. The classification chain runs identically for streaming and non-streaming paths. The engine uses the error category to emit distinct stream-json subtypes instead of generic "streaming error".

### Content Filter Fast-Fail

When a `content_filter` category is detected:

- Emit stream-json `result` with `subtype: "error_content_filter"`
- Do NOT retry; return terminal error

### Quota / Payment Fast-Fail

When a `quota_exhausted` or `payment_required` category is detected:

- Emit stream-json `result` with `subtype: "error_quota_exhausted"` or `"error_payment_required"`
- Do NOT retry; return terminal error

### ModelNotFound Re-Entry (Streaming)

The sticky router's streaming path mirrors the non-streaming fallback logic:

- `model_not_found` → attempt next target (L3 model fallback)
- Exhausted targets → emit `error_model_not_found` subtype and return terminal error

## Category-Aware Routing Decisions

The error category drives the multi-provider router's layer selection (see [multi-provider-routing](./multi-provider-routing.md)):

| Category | Router Action |
|----------|--------------|
| `context_exhausted` | Return to engine (engine handles compaction) |
| `quota_exhausted` / `payment_required` | Skip L1, try L2 key failover |
| `content_filter` | Return immediately (no retry) |
| `rate_limit_*` / `timeout` | L1 retry with category-aware backoff |
| `server_overload` | L1 retry with long backoff |
| `auth` / `permission` | Return immediately (no retry) |
| `model_not_found` | Skip L1/L2, try L3 model fallback |

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| 529 during streaming | Count toward 529 budget; fails after cap (no model fallback) |
| Content filter on HTTP 200 (domestic providers) | Error info populated from body classification; fast-fail |
| ModelNotFound during streaming | Try next target in sticky router; emit error_model_not_found if exhausted |
| 429 with no keyword signal | Classified as `rate_limit_generic`; default backoff applies |

## Acceptance Criteria

- **AC1:** 429 retried with backoff up to max retries.
- **AC2:** Fourth consecutive 529 fails with distinct error message.
- **AC3:** Background classifiers do not retry 529.
- **AC4:** Retry-After honored when set.
- **AC5:** Model and max_tokens preserved across retries.
- **AC6:** `error_content_filter` subtype emitted for content filter errors in streaming.
- **AC7:** `error_quota_exhausted` subtype emitted for quota/payment errors in streaming.
- **AC8:** ModelNotFound triggers L3 fallback in sticky router streaming path.
- **AC9:** Every non-2xx HTTP response is classified into an error category before retry decision.
- **AC10:** Category-aware backoff uses per-category base/max delays.
- **AC11:** 429 is disaggregated into subcategories when keyword signals are present.
