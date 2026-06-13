---
title: Prompt Caching
slug: prompt-caching
priority: P0
status: complete
spec: complete
code: complete
package: internal/api, internal/agent
depends_on:
  - provider-architecture
  - session-resume
  - context-compaction
---
# Prompt Caching

## Overview

Maximize prompt cache hit rate for long-running agent sessions. Target: 98–99% cache hit on sessions beyond 10 turns.

Anthropic's prompt caching uses **prefix matching**: the API compares the incoming request byte-for-byte against cached prefixes. A `cache_control: { type: "ephemeral" }` marker on a content block defines a breakpoint — everything from the start up to and including that block is the cache key. Up to 4 breakpoints per request; minimum 1,024 tokens per cached segment.

The prefix order is fixed: `tools` → `system` → `messages`. Changes to any segment invalidate it and everything downstream.

## Cache Breakpoint Layout

Three breakpoints (of 4 max) are used:

| # | Location | What it covers |
|---|----------|---------------|
| 1 | `system[0]` (frozen prefix) | Intro, memory, tool list, git status, platform/cwd/date, skills manifest, redaction, append prompt |
| 2 | `tools[last]` (last tool definition) | Entire tools array |
| 3 | Last message content block | All prior messages in the conversation |

Breakpoint 4 is reserved for future use (e.g., double-marker for tool call rollback).

### System Prompt — No Dynamic Suffix

The system prompt is a single frozen block:

```
system[0]: { text: "<frozen>", cache_control: { type: "ephemeral" } }
```

`DynamicSystemSuffix` always returns empty. All dynamic content (active skills, cwd/date changes) is communicated through virtual user messages in the message chain. This ensures the system prompt prefix is byte-stable across all turns and across resume, preventing cache invalidation of the entire message chain.

### Message Rolling Cache

The last message's last content block carries `cache_control: { type: "ephemeral" }`. On each turn, the marker moves forward with the growing chain:

```
Turn N:    system[*] + tools[*] + msg[0..X-1] + msg[X(*)]
Turn N+1:  system[*] + tools[*] + msg[0..X-1] + msg[X] + msg[X+1] + msg[X+2(*)]
                                                  ↑ prefix matches cache from Turn N
```

Implementation: `markLastMessageForCaching` in `provider_anthropic.go` scans the built `sdkMessages` from the tail and marks the last content block of the last non-empty message. No state persistence needed.

## Dynamic Content via System Reminders

Instead of a dynamic system prompt suffix, environment changes are injected as virtual user messages (`IsVirtual: true`) in the message chain:

### Mid-session skill activation

When `syncActiveSkills()` detects new skills, a `[system]: Active Skills: ...` virtual message is appended to the chain and persisted as a `system_reminder` transcript entry.

### Post-compaction skill re-injection

After compaction, if `StreamConfig.ActiveSkills` is non-empty, a reminder is injected because the original activation tool results may have been summarized away.

### Resume-time environment changes

`detectResumeChanges()` compares current cwd and date against the frozen system prompt. Differences are injected as virtual messages before the new user message.

### Transcript persistence

`system_reminder` entries are persisted with `Type: system, Subtype: system_reminder`. On resume, `RebuildMessages` restores them as `IsVirtual: true` user messages with `[system]: ` prefix, keeping the message chain structurally identical.

## Compaction Integration

### Insert-then-Compress

Compaction first attempts `inSessionCompact`: appending a compaction instruction as a user message to the existing chain and calling `SendMessage` with the session's system prompt and tools. This shares the cached prefix, so only the instruction itself is cold content.

Fallback to `forkSummaryAgent` (independent call, no cache sharing) when:
- API returns a prompt-too-long error
- Model produces tool_use blocks instead of summary text
- Summary text is empty

### Cost Tracking

Both `inSessionCompact` and `forkSummaryAgent` call `AccumulateUsage` with the compaction response's `Usage`.

### Summary Persistence

`CompactMetadata.Summary` stores the compaction-generated summary text. On resume, `RebuildMessages` restores the boundary as: `[Context boundary: ...]\n\nPrevious summary:\n{summary}`.

## Acceptance Criteria

- **AC1:** Anthropic message content blocks carry `cache_control` on the last block of the last message, achieving >98% cache read ratio after turn 2.
- **AC2:** Compaction response usage is accumulated in cost tracking.
- **AC3:** Compaction summary text is persisted in transcript and restored on resume.
- **AC4:** `DynamicSystemSuffix` always returns empty; dynamic content goes through messages.
- **AC5:** Message cache marker is Anthropic-specific; non-Anthropic providers are unaffected.
- **AC6:** System reminders are persisted in transcript and restored as virtual user messages on resume.
- **AC7:** In-session compaction shares the cached prefix; falls back to independent call on failure.
- **AC8:** Post-compaction and resume-time reminders re-inject active skills and environment changes.
