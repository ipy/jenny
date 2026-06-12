---
title: Thinking Effort, Persistence & DeepSeek Reasoning
slug: thinking-effort-and-persistence
priority: P0
status: done
spec: complete
code: done
package: internal/session,internal/api,internal/cli,internal/agent
gaps: []
depends_on:
  - session-persistence
  - anthropic-api-client
  - openai-api-client
---

# Thinking Effort, Persistence & DeepSeek Reasoning

## Overview

`jenny` supports streaming and rendering `thinking` (Reasoning/Chain of Thought) blocks from multiple providers:

- **Anthropic:** Extended thinking via `thinking.budget_tokens`, with `thinking`/`signature` content blocks
- **OpenAI:** Reasoning effort via `reasoning_effort` (Chat API) or `reasoning_config.effort` (Responses API)
- **DeepSeek:** Reasoning via `extra_body: {"thinking": {"type": "enabled"}}` with `reasoning_content` parsing

This specification addresses the persistence and round-trip requirements for thinking blocks across all supported providers.

## Canonical Acceptance Criteria (AC1–AC5)

### AC1: Thinking Effort Control

The `--effort` CLI flag controls reasoning effort for OpenAI-compatible providers.

#### Responses API (`OPENAI_WIRE_API=responses`)

When `--effort` is set, the request includes `reasoning_config.effort`:

```json
{
  "model": "o3-mini",
  "reasoning_config": {"effort": "high"},
  "input": [{"role": "user", "content": "..."}]
}
```

When `--effort` is not set, no `reasoning_config` is sent (backward compatible).

#### Chat API

When `--effort` is set for non-DeepSeek models, `reasoning_effort` is sent at the top level:

```json
{
  "model": "o3-mini",
  "reasoning_effort": "high",
  "messages": [...]
}
```

### AC2: Thinking Block Control (DeepSeek & Effort Threading)

#### DeepSeek Reasoning Mode

DeepSeek models use `extra_body` to enable thinking:

```json
{
  "model": "deepseek-reasoner",
  "extra_body": {"thinking": {"type": "enabled"}},
  "messages": [...]
}
```

The `reasoning_content` field in the response is captured as a `thinking` block for transcript persistence.

#### Effort Threading

The `--effort` flag threads from CLI → `StreamConfig.Effort` → `ThinkingConfig.Effort` → provider request field. Empty effort means no reasoning configuration is sent.

### AC3: Thinking Persistence in Transcripts

Assistant entries with thinking blocks are persisted to `.jsonl` transcripts with `thinking` and `signature` fields:

```json
{"type":"assistant","content":"The answer is 42.","thinking":"6 * 7 = 42","signature":"sig_abc123"}
```

The `TranscriptEntry` struct in `internal/session/manager.go` includes:

```go
type TranscriptEntry struct {
    // ... existing fields ...
    Thinking  string `json:"thinking,omitempty"`
    Signature string `json:"signature,omitempty"`
}
```

#### Observation: Prompt Caching

Both Anthropic and OpenAI rely on exact prefix matching for cache hits. Persisting thinking blocks verbatim maintains cache eligibility for multi-turn conversations.

### AC4: Thinking Round-Trip for Tool Calls

#### Anthropic

When reconstructing requests from transcripts, assistant messages with non-empty `Thinking` field include a `type: "thinking"` content block **before** `tool_use` blocks:

```json
{
  "role": "assistant",
  "content": [
    {"type": "thinking", "thinking": "...", "signature": "..."},
    {"type": "text", "text": "..."},
    {"type": "tool_use", "id": "...", "name": "...", "input": {...}}
  ]
}
```

The thinking block and its signature must be byte-for-byte identical to prevent 400 errors.

#### OpenAI (Chat API)

Assistant messages with thinking include `reasoning_content` alongside `tool_calls`:

```json
{
  "role": "assistant",
  "reasoning_content": "...",
  "content": "...",
  "tool_calls": [...]
}
```

#### OpenAI (Responses API)

Thinking from previous turns is reconstructed as `reasoning` items in the `input` array:

```json
{
  "input": [
    {"type": "reasoning", "summary": {"type": "text", "text": "Previous thinking..."}},
    {"type": "message", "role": "assistant", "content": [...]}
  ]
}
```

### AC5: Backward Compatibility

Loading older transcripts that lack `thinking` and `signature` fields does not cause parsing errors or crashes. The fields default to empty strings.

## Implementation Details

### CLI Interface

- `--effort <level>`: Sets reasoning effort for OpenAI (`low`, `medium`, `high`)

### Provider Implementation

| Provider | Thinking Config Field | Notes |
|----------|----------------------|-------|
| Anthropic | `thinking.budget_tokens` | Deferred for this iteration |
| OpenAI Chat | `reasoning_effort` | Top-level field |
| OpenAI Responses | `reasoning_config.effort` | When `OPENAI_WIRE_API=responses` |
| DeepSeek | `extra_body.thinking.type` | `extra_body: {"thinking": {"type": "enabled"}}` |

### Package Responsibilities

- **internal/cli:** Parse `--effort` flag, thread to `StreamConfig`
- **internal/api:** Translate `ThinkingConfig` to provider-specific request fields
- **internal/session:** Persist and load `thinking`/`signature` in transcript entries
- **internal/agent:** Extract thinking from API responses, rebuild messages for multi-turn

## Out of Scope

- Anthropic `budget_tokens` configuration (deferred)
- DeepSeek non-tool-call `reasoning_content` omission optimization
- Prompt caching integration tests
- Performance or benchmarking