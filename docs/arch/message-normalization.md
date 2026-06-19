---
title: Message Normalization
slug: message-normalization
priority: P2
status: draft
spec: partial
code: done
package: internal/agent
gaps: []
depends_on:
  - anthropic-api-client
  - universal-normalization-architecture
---
# Message Normalization

## Overview

Before each API request, convert internal transcript messages to API-safe payloads: strip internal fields, merge roles, enforce tool pairing, format Read output.

## Append-Only Guarantee for Prompt Caching

To protect Anthropic prompt caching, message history must be **append-only** across turns — no structural mutations to previously-sent messages.

Two normalization paths exist:

| Path | Scope | Used by | Cache impact |
|------|-------|---------|-------------|
| Per-turn normalization | Content-level only: strip virtual/progress markers, strip orphaned thinking, strip trailing thinking, ensure non-empty assistant | Per-turn engine loop | **Safe** — immutable message boundaries |
| Full normalization | All content fixes + tool result pairing + role merging | Compaction only | **Breaks cache** — structural changes accepted (compaction already destroys cache continuity) |

Key design choice: `mergeConsecutiveSameRole` (the primary cache buster) is **never** called on the normal per-turn path. Previously-sent messages retain their exact byte content and boundaries across turns.

## NormalizeNewMessage (Per-Turn)

Applied to every message before each API request:

1. Strip `IsVirtual` marker
2. Strip `Type == "progress"` 
3. Strip orphaned thinking-only content
4. Strip trailing thinking blocks
5. Ensure non-empty assistant has `[Tool use interrupted]` placeholder

No message filtering, no tool_result pairing, no role merging — content-level only.

## normalizeCompactedChain (Compaction)

Used exclusively after context compaction (`compact.go`). A 5-step pipeline:

1. Filter orphaned thinking-only messages
2. Strip trailing thinking from last assistant
3. Non-empty assistant guard (insert `[Tool use interrupted]` placeholder)
4. Filter whitespace-only messages
5. Ensure tool result pairing

Full structural normalization is acceptable here because compaction already destroys cache continuity by changing the message array. Note: unlike `NormalizeMessagesAPI` (which exists only in tests), this pipeline does **not** include `filterInternalMessages` or `MergeConsecutiveSameRole`.

This doc covers agent-package normalization only. API-level normalization (`api.NormalizeMessages`) runs on every provider `SendMessage` call and handles additional role merging and content block validation — see [`anthropic-api-client.md`](./anthropic-api-client.md).

## Strip Internal Content

Drop from API send:

- `progress`, most system subtypes (except allowed).
- Synthetic API error messages.
- Virtual (`isVirtual`) user/assistant messages.
- Non-API fields on tool_use blocks.

## Tool Result Pairing

Tool result pairing:

| Direction | Action |
|-----------|--------|
| Forward | Synthetic error result for missing tool_use_id |
| Reverse | Strip orphaned tool_results |
| Duplicate IDs | Dedupe across messages |
| Leading orphaned user tool_result | Strip or placeholder text |
| Empty assistant after strip | Insert `[Tool use interrupted]` text |


## Role Merging

- Consecutive user messages → merge (Bedrock compatibility).
- Consecutive assistant with same `message.id` → merge streaming chunks.

## Read Output Format

- `offset=1` default (1-based); `offset=0` → line 1.
- Line numbers: 6-char right-padded format (`%6s\t%s`).
- Empty content: warning, not error.
- Past EOF: warning with actual line count.

## Media Error Mapping

| API pattern | User-facing string |
|-------------|-------------------|
| Image size / resize | getImageTooLargeErrorMessage |
| PDF page limit | getPdfTooLargeErrorMessage |
| Password PDF | getPdfPasswordProtectedErrorMessage |
| Invalid PDF | getPdfInvalidErrorMessage |
| 413 request too large | getRequestTooLargeErrorMessage |

On media errors during API calls, `HandleMediaErrorOnRetry` identifies the offending tool_result using `FindLargestMediaToolUseID` (heuristic: largest content in the last user message's tool_results), strips it, and returns modified messages for retry.

## Thinking Normalization Order

1. Orphaned thinking filter
2. Trailing thinking strip
3. Non-empty assistant guard (must run BEFORE whitespace filter so placeholder is inserted first)
4. Whitespace-only filter
5. Tool pairing

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| is_error tool_result | Inner content text-only |
| Resume mid-turn tool_result only | Repair without assistant-first payload |

## Acceptance Criteria

- **AC1:** No internal UUID/timestamp in API JSON.
- **AC2:** Every tool_use has matching tool_result.
- **AC3:** Read uses fixed-width line numbers.
- **AC4:** Media errors map to specific strings.
- **AC5:** Last assistant never ends with thinking block.
