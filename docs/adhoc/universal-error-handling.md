---
title: Universal LLM API Error Handling
slug: universal-error-handling
status: phase4
Phase 3: done
date: 2025-07-10
package: internal/api
gaps:
  - Provider-specific keyword expansion deferred to Phase 2
  - Domestic provider business codes ignored
  - Engine integration deferred to Phase 6
depends_on:
  - rate-limit-handling
  - multi-provider-routing
  - provider-architecture
---

# Universal LLM API Error Handling — Ad-hoc Migration Plan

## Background

The current error handling in Jenny's API layer relies on HTTP status codes plus a small set of hardcoded keywords. This works for simple cases (401 → auth, 429 → retry, 500 → retry) but breaks down across 19 LLM API providers because:

- **HTTP 400** carries 5+ distinct semantics: invalid parameter, context exhausted, content filter, quota exhausted (Arrearage), rate limit throttling.
- **HTTP 429** mixes rate limit (retryable), quota exhausted (not retryable), and server overload (long backoff retryable).
- **HTTP 413** means context exhausted for Claude, Fireworks, OpenRouter, Groq — but Jenny's `isPromptTooLong*` functions never check 413.
- **HTTP 498** (Groq Flex Tier capacity exceeded) is retryable but not in `isRetryable()`.
- **Domestic providers** (讯飞, 智谱, MiniMax, 阿里百炼) use business codes inside JSON response bodies that map to completely different semantics than the HTTP status code alone suggests.
- **Gemini** may return 500 INTERNAL for context-too-long errors rather than 400.

Research data: 19 providers' error codes documented in `.jenny/llm-api-error-codes-research.md`.

## Current Code Baseline

| File | Role | Limitation |
|------|------|-----------|
| `internal/api/retry.go` | `isRetryable(statusCode, err)` — sole retry decision gate | Only knows 429, 5xx, 408, 409; misses 498; treats all 429 alike |
| `internal/api/http.go` | `HTTPError` struct — `{StatusCode, Message, ShouldRetry}` | No semantic category field |
| `internal/api/stream.go` | `StreamResult` struct — `{IsPermanent, ContextRejected, MaxTokensErr}` | Three boolean/pointer fields are the only classification channel |
| `internal/api/provider_anthropic.go` | `isPromptTooLongAnthropic()` | Matches only `prompt_too_long` and `context window exceeds limit` |
| `internal/api/provider_openai.go` | `isPromptTooLongOpenAI()` | Matches only `prompt_too_long` and `context window exceeds limit` |
| `internal/api/provider_genai.go` | `isPromptTooLongGenAI()` | Same 2 keywords |
| `internal/api/provider_openai_responses.go` | `isPromptTooLongOpenAIResponses()` | Same 2 keywords |
| `internal/api/router/sticky.go` | L1 retry → L2 key failover → L3 model fallback | Decides retry based on status code ranges, not semantic category |
| `internal/api/client.go` | `CategoryContextExhausted` / `MaxTokensError` | Existing context-exhausted prototype |
| `internal/agent/engine_loop.go` L498-536 | Context-exhausted → auto-compaction | Only triggered via `MaxTokensErr`; no quota/content-filter handling |

## ErrorCategory Enum

Add to `internal/api/client.go` alongside existing `CategoryContextExhausted`:

```go
type ErrorCategory string

const (
    CategoryUnknown            ErrorCategory = "unknown"
    CategoryAuth               ErrorCategory = "auth"
    CategoryPermission         ErrorCategory = "permission"
    CategoryInvalidRequest     ErrorCategory = "invalid_request"
    CategoryContextExhausted   ErrorCategory = "context_exhausted"
    CategoryRateLimitRPM       ErrorCategory = "rate_limit_rpm"
    CategoryRateLimitTPM       ErrorCategory = "rate_limit_tpm"
    CategoryRateLimitConcurrency ErrorCategory = "rate_limit_concurrency"
    CategoryRateLimitGeneric   ErrorCategory = "rate_limit_generic"
    CategoryQuotaExhausted     ErrorCategory = "quota_exhausted"
    CategoryPaymentRequired    ErrorCategory = "payment_required"
    CategoryContentFilter      ErrorCategory = "content_filter"
    CategoryServerOverload     ErrorCategory = "server_overload"
    CategoryServerError        ErrorCategory = "server_error"
    CategoryTimeout            ErrorCategory = "timeout"
    CategoryCancelled          ErrorCategory = "cancelled"
    CategoryModelNotFound      ErrorCategory = "model_not_found"
)
```

## Struct Changes

### HTTPError (`internal/api/http.go`)

```go
type HTTPError struct {
    StatusCode    int
    Message       string
    ShouldRetry   *bool
    ErrorCategory ErrorCategory  // NEW
}
```

### StreamResult (`internal/api/stream.go`)

```go
type StreamResult struct {
    // ... existing fields ...
    ErrorCategory ErrorCategory  // NEW
}
```

## classifyErrorCommon — Universal Keyword Layer

Add to `internal/api/client.go` (or new file `internal/api/error_classify.go`). All providers call this first, then overlay provider-specific logic.

### Classification Priority Order

1. **413** → `CategoryContextExhausted` (Claude/Fireworks/OpenRouter/Groq use 413 for context limits)
2. **402** → `CategoryPaymentRequired` (7/19 providers: DeepSeek/Claude/Mistral/Cohere/OpenRouter/Fireworks/Cerebras)
3. **400/500/504** keyword scan — context keywords first (highest priority because 400 mixes 5+ semantics)
4. **Content filter keywords**
5. **Quota/payment keywords**
6. **429 disaggregation** — overload → quota → RPM → TPM → concurrency → generic
7. **5xx mapping** — 529→overload, 503→overload, 504→timeout, 498→overload, else→server_error
8. **401→auth, 403→permission, 499→cancelled, 404→model_not_found**
9. **400 default→invalid_request**

### Context Keywords (21 items, sourced from 19 providers)

| Keyword | Provider Source |
|---------|----------------|
| `context_length_exceeded` | OpenAI error.code, OpenRouter error_type |
| `prompt_too_long` | Anthropic error.type |
| `context window exceeds limit` | Anthropic error.message |
| `maximum context length` | OpenAI error.message |
| `too many tokens` | Cohere |
| `size limit exceeded` | Cohere |
| `token limit exceeded` | Mistral |
| `input token length too long` | Kimi |
| `context length exceeded` | Generic |
| `prompt length exceeded` | 阿里百炼 |
| `chat context length exceeded` | 阿里百炼 |
| `input data length exceeded` | 阿里百炼 |
| `payload_too_large` | OpenRouter error_type (413) |
| `request_too_large` | Claude error.type (413) |
| `exceed model token limit` | Kimi |
| `token数量超过上限` | 讯飞 10907/10910 |
| `上下文超长` | 讯飞 10012 |
| `上下文超限` | 国内供应商 |
| `prompt超长` | 智谱 1261 |
| `range of input length` | 阿里百炼 |
| `total message token length` | 阿里百炼 |

### Content Filter Keywords

| Keyword | Provider Source |
|---------|----------------|
| `content_policy_violation` | OpenRouter error_type |
| `content_filter` | OpenAI error.code |
| `safety` | Claude error.type, Gemini |
| `refusal` | OpenRouter error_type, Anthropic |
| `inappropriate` | Generic |
| `offensive` | Generic |
| `敏感内容` | 讯飞 10013/10014 |
| `DataInspectionFailed` | 阿里百炼 error.type |
| `FaqRuleBlocked` | 阿里百炼 error.type |
| `CustomRoleBlocked` | 阿里百炼 error.type |

### Quota / Payment Keywords

| Keyword | Provider Source |
|---------|----------------|
| `Arrearage` | 阿里百炼 error.code (under 400!) |
| `quota` | OpenRouter token_limit_exceeded |
| `payment_required` | OpenRouter error_type |
| `insufficient_quota` | OpenAI error.code |
| `billing` | DeepSeek, Cerebras |
| `次数超限` | 讯飞 11201 |
| `余额不足` | 智谱 1113/1304 |
| `exceed quota` | MiniMax |

### 429 Disaggregation Logic

When statusCode == 429, check in order:

1. **ServerOverload** — message contains `overload` / `繁忙` / `排队` / `capacity` / `busy` / `heavy load`, OR Retry-After > 30s, OR OpenRouter error_type == `provider_overloaded`
2. **QuotaExhausted** — message contains `quota` / `insufficient` / `余额` / `次数` / `limit exceeded` (token/quota, not context), OR 阿里百炼 Arrearage under 400
3. **RateLimitRPM** — message contains `rate` / `rpm` / `requests per` / `秒级流控`, OR 讯飞 11202
4. **RateLimitTPM** — message contains `token per` / `tpm` / `tokens per minute`, OR 讯飞 11210
5. **RateLimitConcurrency** — message contains `concurrent` / `并发` / `simultaneous`, OR 讯飞 11203/10006/10007, 智谱 1302, MiniMax 1041
6. **RateLimitGeneric** — fallback when no subtype keyword matches

## Provider-Specific Classifiers

Each provider implements a `classifyErrorXxx(statusCode, body)` that returns `CategoryUnknown` when it has no match, letting `classifyErrorCommon` handle the rest. Call order: provider-specific first → common fallback.

### 讯飞 (`provider_xfyun.go` or inline)

Business code in `json.code` field:
- 10012 → context-exhausted if msg contains 超长/token, else server-overload (dual semantics)
- 10907, 10910 → context-exhausted
- 10013, 10014, 10019 → content-filter
- 11201 → quota-exhausted (次数超限)
- 11202 → rate-limit-RPM (秒级流控)
- 11203 → rate-limit-concurrency (并发流控)
- 11210 → rate-limit-TPM (tpm超限 — **NOT context-exhausted**, previous record was wrong)
- 10006, 10007 → rate-limit-concurrency
- 10008, 10010, 10110 → server-overload
- 10015, 10016, 11200 → auth
- 11221 → model-not-found (套餐不支持)

### 智谱 (`provider_zhipu.go` or inline)

Business code in `json.error_code` field:
- 1261 → context-exhausted (Prompt超长)
- 1301 → content-filter
- 1113, 1304, 1308, 1309, 1310 → quota-exhausted
- 1302 → rate-limit-concurrency
- 1303, 1305, 1313 → rate-limit-generic
- 1312 → server-overload (模型访问量过大)
- 1000-1004 → auth
- 1110, 1112, 1121, 1220 → permission
- 1311, 1211, 1212, 1221, 1222 → model-not-found

### MiniMax (`provider_minimax.go` or inline)

Business code in `json.base_resp.status_code`:
- 1039 → context-exhausted
- 1026, 1027 → content-filter
- 1008, 2056 → quota-exhausted
- 1002, 2045 → rate-limit-generic
- 1041 → rate-limit-concurrency
- 1004, 2049 → auth

### 阿里百炼 (`provider_bailian.go` or inline)

Code/type in `json.code` + `json.type`:
- Arrearage → quota-exhausted (under HTTP 400!)
- DataInspectionFailed, FaqRuleBlocked, CustomRoleBlocked → content-filter (under 400)
- Range of input length / Total message token length → context-exhausted (under 400)
- Throttling.RateQuota → rate-limit-RPM; Throttling.AllocationQuota → rate-limit-TPM

### OpenRouter

Typed `error.metadata.error_type` — the most mature classification system across all 19 providers. Direct mapping:
- context_length_exceeded, max_tokens_exceeded, payload_too_large → context-exhausted
- content_policy_violation, refusal → content-filter
- rate_limit_exceeded → rate-limit-generic (+ Retry-After header)
- payment_required, token_limit_exceeded → quota-exhausted
- provider_overloaded → server-overload (+ Retry-After header)
- authentication → auth; permission_denied → permission
- invalid_request, invalid_prompt, string_too_long, unprocessable → invalid-request
- not_found → model-not-found; provider_unavailable → server-error; timeout → timeout; server → server-error

### AWS Bedrock

AWS-standard `json.__type` field:
- IncompleteSignature → auth (under 400!)
- NotAuthorized → permission (under 400!)
- AccessDeniedException → permission; InvalidClientTokenId → auth (under 403)
- ThrottlingException → rate-limit-generic; ServiceUnavailable → server-overload
- ValidationError → invalid-request (context limit may be inside — fall through to keyword scan)

### Fireworks

- 429: serverless=rate-limit, dedicated/capacity=server-overload (check message for "dedicated"/"capacity")
- 413 → context-exhausted
- 412: "suspend"/"account" → quota-exhausted, else → invalid-request
- 500 described as "unlikely to self-recover bug" — still maps to server-error per convention

### Groq

- 498 → server-overload (Flex Tier capacity, retryable — **add to isRetryable**)
- 413 → context-exhausted
- 499 → cancelled (request cancelled by caller)
- error.type "invalid_request_error" → invalid-request

### Gemini

- 500/504 may be context-exhausted rather than server-error. Provider-specific `classifyGenAIError` checks message for "context"+"long" or "token"+"limit" when status is 500/504.

## Replace isPromptTooLong

Current `isPromptTooLong*` functions in 4 provider files → replaced by unified `classifyError` returning `ErrorCategory`. `ContextRejected` field preserved for backward compat, driven by `ErrorCategory == CategoryContextExhausted`.

## Sticky Router Integration (`internal/api/router/sticky.go`)

Current L1/L2/L3 logic uses status code ranges. Replace with ErrorCategory-driven switch:

| Category | Router Action |
|----------|--------------|
| ContextExhausted | Return to engine (engine handles compression) |
| QuotaExhausted / PaymentRequired | Skip L1, try L2 key failover |
| ContentFilter | Return immediately (no retry) |
| RateLimitRPM/TPM/Concurrency/Generic | L1 retry with category-aware backoff |
| ServerOverload | L1 retry with long backoff (30-120s) |
| Auth / Permission | Skip L1, try L2 different key |
| ModelNotFound | Skip L1/L2, try L3 model fallback (cross-provider via profile.Targets chain) |

**Streaming limitation**: `StickyClient.SendMessageStream` delegates directly to provider client without retry logic. For ModelNotFound during streaming, Engine must detect `ErrorCategory` in StreamResult and re-call `SendMessage` to trigger full fallback chain.

## Engine Integration (`internal/agent/engine_loop.go`)

Extend current `MaxTokensErr == CategoryContextExhausted` path to also check `streamResult.ErrorCategory`:

- ContextExhausted → existing auto-compaction path
- QuotaExhausted / PaymentRequired → fast-fail with user-friendly message
- ContentFilter → fast-fail with moderation message
- ModelNotFound → return `ModelNotFoundError` for router re-entry

## isRetryable Update (`internal/api/retry.go`)

Priority: ErrorCategory-driven judgment > HTTP status code fallback.

Add 498 to status code fallback list. When `HTTPError.ErrorCategory` is set, use category mapping instead of raw status codes.

## Backoff Strategy (`internal/api/router/sticky.go`)

| Category | Base Delay | Max Delay |
|----------|-----------|-----------|
| RateLimitRPM | 2s | 15s |
| RateLimitTPM | 5s | 30s |
| RateLimitConcurrency | 10s | 60s |
| RateLimitGeneric | 1s | 32s |
| ServerOverload | 15s | 120s |
| ServerError | 500ms | 32s |
| Timeout | 2s | 30s |

Jitter: use `rand.New(rand.NewSource(seed))` with injectable seed — production uses `time.Now().UnixNano()`, tests inject fixed seed for determinism.

Honor `Retry-After` header (OpenRouter, Claude provide this).

## Implementation Phases

### Phase 1: Emergency keyword expansion (minimal diff)

- Expand `isPromptTooLong*` keyword lists in all 4 provider files
- Add `statusCode == 413` check to `isPromptTooLong*`
- Files: `provider_anthropic.go`, `provider_openai.go`, `provider_genai.go`, `provider_openai_responses.go`

### Phase 2: ErrorCategory enum + classifyErrorCommon [done]

- [x] Define `ErrorCategory` in `client.go`
- [x] Add `ErrorCategory` field to `HTTPError` and `StreamResult`
- [x] Implement `classifyErrorCommon` in new `error_classify.go`
- [x] Replace `isPromptTooLong*` calls with `classifyError`
- [x] Update isRetryable with category priority + 498

### Phase 3: Sticky Router category-aware decisions

- Modify `sticky.go` L1/L2/L3 logic to use ErrorCategory switch
- Implement `computeBackoffForCategory`
- Support Retry-After header

### Phase 4: Domestic provider business code parsers

- `classifyErrorXfyun`, `classifyErrorZhipu`, `classifyErrorMiniMax`, `classifyErrorBailian`
- May need new provider files

### Phase 5: International provider-specific classifiers

- OpenRouter error_type, AWS Bedrock __type, Fireworks dual-mode 429, Groq 498/413/499

### Phase 6: Engine-level recovery

- Category-aware error handling in `engine_loop.go`
- Quota fast-fail, content-filter message, ModelNotFound re-entry

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| ErrorCategory on `HTTPError` vs new error type | Add field to existing struct | All providers already return `HTTPError`; no error chain unwrapping changes needed |
| ErrorCategory layer vs `isRetryable` patch | New semantic layer above `isRetryable` | 400/429 are multi-semantic — need "what does this mean", not "is it retryable" |
| 413 → `CategoryContextExhausted` | Safe | All 4 providers using 413 mean context limit; compression path harmless even on false match |
| 讯飞 10012 dual semantics | Keyword disambiguation | `超长`/`token` → context-exhausted; else → server-overload |
| `ModelNotFound` cross-provider fallback | L3 via `nextTargetLocked` | `profile.Targets` chain is provider-agnostic; streaming needs Engine re-entry |

## Research Data

Full 19-provider error code table with unified 9-category mapping: `.jenny/llm-api-error-codes-research.md`
Current code analysis: `.jenny/current-error-handling-analysis.md`

## Consolidation Plan

After implementation is complete and verified:
- Merge the ErrorCategory enum, classification rules, and provider-specific mappings into `docs/arch/rate-limit-handling.md` (upgrade from current simple retry spec to full semantic error handling spec)
- Delete this adhoc file