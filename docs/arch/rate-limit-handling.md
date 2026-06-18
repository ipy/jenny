---
title: Rate Limit Handling & Universal Error Classification
slug: rate-limit-handling
priority: P1
status: done
spec: complete
code: done
package: internal/api
gaps: []
depends_on:
  - anthropic-api-client
  - universal-error-handling
---
# Rate Limit Handling

## Overview

API client retries transient failures with exponential backoff. Foreground agent calls retry 529 overload errors; background classifiers do not.

## Retryable Conditions

| Condition | Retry |
|-----------|-------|
| HTTP 429 | Yes |
| HTTP 529 / overloaded_error | Yes (foreground, capped) |
| HTTP 408, 409 | Yes |
| HTTP 5xx | Yes |
| Connection errors | Yes (transient only: timeouts, temporary DNS failures; NOT connection refused, host unreachable) |
| Mock rate limits | No |
| Non-retryable 4xx | No |
| Subscriber 429 with x-should-retry: false | No |

## Backoff

- Base delay: 500ms
- Exponential cap: 32s
- Jitter: 25%
- Honor `Retry-After` header when present
- Default max retries: 10 (env override)

## 529 Overload Cap

`MAX_529_RETRIES = 3` consecutive 529 errors:

- Then throw `CannotRetryError` with message **Repeated 529 Overloaded errors**.
- No automatic fallback to a different model.

## Foreground vs Background

Background query sources (classifiers, summaries, memory extraction):

- **No** 529 retry (immediate `CannotRetryError`).
- Prevents retry amplification on auxiliary calls.

Foreground sources (main agent loop): full 529 retry budget.

## Retry Context Preservation

Each retry passes unchanged:

- `model`
- `thinkingConfig`
- `maxTokensOverride` (adjusted on max-output overflow)
- `fastMode`

## Max Output Token Recovery

On max-output-tokens stop reason:

- Retry with increased `maxTokensOverride` (bounded).
- Cap recovery retries to avoid infinite loops (see agent loop).

## Engine-Level Recovery (Phase 6)

### ErrorInfo Propagation in Streaming

All providers set `StreamResult.ErrorInfo` for non-2xx HTTP responses before returning. This propagates the normalized error category to the engine's streaming loop:

```
HTTP response → HTTPError → classifyDomestic → classifyInternational → classifyCommon → ErrorInfo
```

Providers populate `StreamResult.ErrorInfo` with the fully resolved category, cleaned message, and status code. The engine uses `ErrorInfo` to emit distinct stream-json subtypes instead of generic "streaming error".

### Content Filter Fast-Fail

When `StreamResult.ErrorInfo.Category == CategoryContentFilter`:

- Emit stream-json `result` with `subtype: "error_content_filter"`
- Do NOT retry; return terminal error
- Increment compact failure counter (skip for user abort)

### Quota Exhausted Fast-Fail

When `StreamResult.ErrorInfo.Category == CategoryQuotaExhausted`:

- Emit stream-json `result` with `subtype: "error_quota_exhausted"`
- Do NOT retry; return terminal error
- Increment compact failure counter (skip for user abort)

### ModelNotFound Re-Entry (Streaming)

The sticky router's streaming `SendMessageStream` mirrors the non-streaming fallback logic:

- `CategoryModelNotFound` → attempt next target (L3 model fallback)
- Exhausted targets → emit `error_model_not_found` subtype and return terminal error

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| 529 during streaming | Count toward 529 budget; fails after cap (no model fallback) |
| Budget exhausted mid-retry | Stop with distinct error |
| Content filter on HTTP 200 (domestic providers) | ErrorInfo populated from body classification; fast-fail |
| ModelNotFound during streaming | Try next target in sticky router; emit error_model_not_found if exhausted |

## Acceptance Criteria

- **AC1:** 429 retried with backoff up to max retries.
- **AC2:** Fourth consecutive 529 fails with distinct error message.
- **AC3:** Background classifiers do not retry 529.
- **AC4:** Retry-After honored when set.
- **AC5:** Model and max_tokens preserved across retries.
- **AC6:** `error_content_filter` subtype emitted for CategoryContentFilter in streaming.
- **AC7:** `error_quota_exhausted` subtype emitted for CategoryQuotaExhausted in streaming.
- **AC8:** ModelNotFound triggers L3 fallback in sticky router streaming path.
