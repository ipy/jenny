---
title: Pinned Messages
slug: pinned-messages
priority: P2
status: done
spec: complete
code: done
package: internal/api, internal/agent, internal/session
depends_on:
  - context-compaction
  - session-persistence
---
# Pinned Messages

## Overview

Pinned messages survive context compaction verbatim. They are never dropped and never folded into the compaction summary. This preserves the user's original task, compaction boundary summaries, and other critical constraints that would otherwise drift when older turns are summarized.

Jenny is a headless agent. There is no interactive pin command. Pinning is automatic or driven by external protocols.

## Message Metadata

Each conversation message may carry a local `pinned` boolean. The flag is:

- Stored in the JSONL transcript on chain-participant entries
- Restored on session resume when the conversation chain is rebuilt
- Omitted from provider API requests (local metadata only; same treatment as other internal fields)

Pinned messages participate in the normal message chain. They do not alter the system prompt or prompt-cache prefix.

## Automatic Pinning

| Trigger | Behavior |
|---------|----------|
| First user message | The original task prompt in a new session is pinned |
| Compaction boundary | The `[Context boundary: …]` summary message is pinned after each compaction |
| SDK / external input | Consumers of the SDK input protocol may mark messages pinned (future-facing; not implemented in the initial release) |

## Pin Limit

At most **10** pinned messages per session. When the limit would be exceeded, the oldest pinned message that is **not** a compaction boundary is unpinned. Boundary markers are never unpinned by the limit enforcement.

## Compaction Integration

When building the post-compaction chain:

1. Pinned messages from the pre-compaction chain are separated from non-pinned messages
2. Token cost of all pinned messages is estimated
3. Remaining tail budget = target tail budget minus pinned token cost minus summary overhead
4. Non-pinned messages are selected from the tail walking backward, respecting tool_use/tool_result pairing (same rules as [context-compaction](./context-compaction.md))
5. If pinned messages alone exceed the target budget, only pinned messages plus the new boundary are kept (no additional tail)
6. Final order: **compaction boundary (pinned) → other pinned messages (chronological) → recent tail**

The compaction summary prompt lists pinned messages that are excluded from summarization so the model does not redundantly restate their content.

## Persistence

The `pinned` field is written on transcript entries for user messages, system reminders, and compaction boundaries. Resume rebuild restores the flag onto reconstructed API messages. Compaction boundary entries are always persisted with `pinned: true`.

## Acceptance Criteria

- **AC1:** First user message in a new session is pinned and persisted with `pinned: true`.
- **AC2:** Compaction boundary message is pinned in the live chain and persisted with `pinned: true`.
- **AC3:** Pinned messages are never dropped or summarized during compaction.
- **AC4:** Post-compaction chain order is boundary → older pinned → tail.
- **AC5:** Compaction summary prompt references pinned messages excluded from summarization.
- **AC6:** Tail token budget accounts for pinned message token cost.
- **AC7:** Pin limit of 10 enforced; oldest non-boundary pinned message unpinned when exceeded.
- **AC8:** `pinned` survives transcript round-trip and resume rebuild.
- **AC9:** `pinned` is not sent to provider APIs.

## Related

- [context-compaction](./context-compaction.md) — compaction thresholds and boundary persistence
- [session-persistence](./session-persistence.md) — JSONL transcript format
- [session-resume](./session-resume.md) — chain rebuild on resume
- [dynamic-max-tokens](./dynamic-max-tokens.md) — complexity evaluation
