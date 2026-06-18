---
title: Prompt Caching
slug: prompt-caching
priority: P0
status: done
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
| 1 | `system[-1]` (last system block) | All system prompt blocks (Intro, memory, tools, platform, cwd, date, git, skills, redact, append) |
| 2 | `tools[last]` (last tool definition) | Entire tools array |
| 3 | Last message content block | All prior messages in the conversation |

### Multi-Block System Prompt

The system prompt is split into 4 logical blocks ordered by stability (see [`system-prompt.md`](./system-prompt.md)). This maximizes cross-project cache hits for global/machine sections (Blocks 1 & 2) while isolating project-specific or volatile sections (Blocks 3 & 4).

The Anthropic provider applies `cache_control` to the **last block** of the system prompt:

```
system[0]: { text: "<block1>" }
system[1]: { text: "<block2>" }
system[2]: { text: "<block3>" }
system[3]: { text: "<block4>", cache_control: { type: "ephemeral" } }
```

Rationale: Marking the last block ensures that everything before it (the entire system prompt) is included in the cache key. Since the blocks are stability-ordered, a change in a late block (e.g., Block 4's Git status) doesn't invalidate the earlier blocks for cross-session/cross-project matching.

### System Prompt — No Dynamic Suffix

`DynamicSystemSuffix` always returns empty. All dynamic updates that occur *after* session start are communicated through **System Reminders** in the message chain. This keeps the system prompt prefix byte-stable across all turns and across resume.

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

`detectResumeChanges()` compares current cwd, date, and skills list against the frozen system prompt. All detected differences are merged into a **single** `<system-reminder>` block and injected as one virtual user message before the new user prompt:

```xml
<system-reminder>
The current date is 2026-06-16.
The working directory is now /Users/sin/work/agents/jenny.
New skills available:
- agent-browser: Browser automation for AI agents
- ultra-goal: Orchestrate multi-agent workflows for complex engineering goals
Skills removed: find-skills
</system-reminder>
```

The cache is **never busted** — all changes flow through the message chain.

#### Skills change detection on resume

When resuming, `extractSkillsFromManifest()` parses the frozen system prompt's "Available Skills" section (name → description) and compares it against `cfg.Skills`. Both name and description are compared; description changes also trigger a reminder. This preserves prompt caching benefits for unchanged skills while ensuring the agent always sees the current skill set.

### Transcript persistence

`system_reminder` entries are persisted with `Type: system, Subtype: system_reminder` and store the full `<system-reminder>` XML content. On resume, `RebuildMessages` restores them as `IsVirtual: true` user messages, keeping the message chain structurally identical.

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
