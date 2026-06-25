---
title: Side Query Framework
slug: side-query
priority: P1
status: done
spec: complete
code: done
package: internal/agent
depends_on:
  - prompt-caching
  - dynamic-max-tokens
  - progress-monitor
  - multi-provider-routing
---
# Side Query Framework

## Overview

The Side Query framework (`internal/agent/sidequery.go`) provides a unified abstraction for all non-main-loop API calls: complexity evaluation, progress evaluation, and context compaction summaries. It supports two execution modes — **Fork** and **Summary** — with automatic cost-based mode selection.

## Design Principles

1. **Cache prefix immutability:** Fork-mode queries must use the exact same cache prefix as the main session. This means identical model, `thinking.budget_tokens`, system prompt, tools, and message ordering.
2. **Model resolution:** All side query clients resolve model names via `ResolveModel()` (`internal/agent/model_resolve.go`), ensuring aliases like `sonnet`, `opus`, `haiku` are mapped to concrete identifiers.
3. **Cost accumulation:** All side query API responses are accumulated in the shared `CostState` via `AccumulateUsage`, protected by the engine's mutex.
4. **Fallback safety:** When the preferred mode fails, the framework automatically retries with the other mode if available.

## SideQueryContext

Created once at session start in `NewQueryEngine` and populated after the system prompt is frozen:

```go
type SideQueryContext struct {
    SystemPrompt   []string           // Frozen system prompt blocks
    Tools          []api.ToolParam    // Frozen tool definitions
    Model          string             // Resolved model name
    RequestedModel string             // Original model before resolution
    ThinkingConfig api.ThinkingConfig // Fixed thinking config (immutable budget)
    CostState      *CostState         // Shared cost accumulator
    CostMu         *sync.Mutex        // Protects CostState
    StreamCfg      *StreamConfig      // Session configuration
}
```

The `ThinkingConfig` captures the session's fixed thinking budget, ensuring fork clients share the same `budget_tokens` for cache key compatibility.

## Execution Modes

### Fork Mode (`SideQueryModeFork`)

Reuses the full conversation prefix. Ideal when prompt caching is available and the prefix is already cached.

- Creates a new API client with the main session's model and thinking config
- Copies the full message history and appends the evaluation prompt
- Sends with the main session's system prompt and tools
- **Cache impact:** The entire prefix is a cache read; only the appended prompt is new

### Summary Mode (`SideQueryModeSummary`)

Condenses the conversation into a shorter request. Useful for cheaper models or when the full prefix would exceed context limits.

**Message flattening:** Instead of forwarding the multi-turn message history as-is (which LLMs summarize poorly due to interleaved roles, tool calls, and tool results), Summary mode renders the entire conversation into a single plain-text transcript and sends it as one user message. The rendering:

- Labels each message with its role (`[user]`, `[assistant]`, `[tool_use: <name> {input}]`, `[tool_result]`)
- Includes serialized JSON input parameters for tool_use blocks (truncated to 500 chars to avoid bloating the summary with large file contents)
- Strips media (images/documents replaced with markers), thinking blocks, and signatures
- Truncates large tool results to 2000 characters
- Optionally limits to the last N messages (`SummaryMaxMessages`) before rendering
- Combines the rendered transcript with the evaluation prompt into the single user message

**Single-message structure:** The API call sends exactly one user message containing: (1) the rendered conversation transcript, and (2) the `Prompt` field (if set). The `SummarySystemPrompt` carries the task instructions and output format requirements.

- Can use a different (cheaper) model (`SummaryModel`)
- Can disable thinking (`DisableThinking`)
- Uses its own system prompt (`SummarySystemPrompt`)
- No tools are sent — `nil` tool list
- **Cache impact:** No cache sharing with the main session

## Cost-Based Mode Selection

`SelectMode()` estimates the cost of both modes and picks the cheaper one:

**Fork cost estimate:**
```
cached_tokens × CacheReadUSD + new_tokens × CacheCreationUSD + MaxTokens × OutputUSD
```
Where `new_tokens` = eval prompt tokens + 200 overhead; `cached_tokens` = total input - new_tokens.

**Summary cost estimate:**
```
input_tokens × InputUSD + MaxTokens × OutputUSD
```
Where `input_tokens` reflects the truncated/filtered message set.

Pricing data comes from `GetModelPricing()` in `cost.go`.

## Current Users

| Caller | Mode | Purpose |
|--------|------|---------|
| `dispatchComplexityEval` | Fork only | Classify task complexity (4 dimensions + need_write) |
| `dispatchProgressEval` | `Execute()` (auto-select) | Judge whether the agent is making meaningful progress |
| `fallbackSummaryCompact` | `ExecuteSummary()` | Fallback compaction — thinking disabled, `fallbackCompactSystemPrompt` |
| `inSessionCompact` | Main client directly | In-session compaction reuses main client for cache sharing |

Complexity eval uses `ExecuteFork()` because it always needs cache-sharing and fires early (short prefix). Progress eval uses `Execute()` with `SelectMode` to choose between fork (cache-sharing) and summary (cheaper for long conversations) based on estimated cost. `fallbackSummaryCompact` uses `ExecuteSummary()` with `DisableThinking: true`.

## Compact Integration

`fallbackSummaryCompact` delegates to `SideQuery.ExecuteSummary()`:
- Each attempt creates a `SideQuery` with the current (possibly dropped) messages
- `ExecuteSummary` creates its own API client via `SideQueryContext.ClientFactory` (no mutation of the main client)
- Thinking is disabled via `SideQuery.DisableThinking`
- Retry logic (proportional 25% message dropping on prompt-too-long) stays in `compact.go`
- `SideQueryContext.ClientFactory` enables test injection of mock clients

`inSessionCompact` uses the main client directly because it intentionally shares the session's exact cache prefix (system prompt + tools + message history).

Both `inSessionCompactPrompt` and `fallbackCompactSystemPrompt` share the same `compactSummaryRequirements` constant, ensuring consistent output format regardless of which compaction path succeeds.

## Invariants

- Fork mode always uses the exact same `ThinkingConfig` as the main session (fixed `budget_tokens`)
- Fork mode always resolves the model via `ResolveModel()`
- Cost from all side queries is accumulated in the shared `CostState`
- `SideQueryContext` is populated once and never modified after initialization
- Fork failures do not crash the session — they are logged and callers handle the error
