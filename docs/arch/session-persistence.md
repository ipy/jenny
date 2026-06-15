---
title: Session Persistence
slug: session-persistence
priority: P0
status: done
spec: complete
code: done
package: internal/session
gaps: []
depends_on:
  - session-id-stability
---
# Session Persistence

## Overview

Jenny persists conversation state as append-only JSONL transcripts under the project directory. Transcripts are the source of truth for resume, cost restoration, and headless consumers that round-trip `session_id`.

## Envelope Fields

Every line in a transcript JSONL file carries two mandatory envelope fields:

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session ID; a lowercase UUID v4 string, equal to the JSONL filename stem (filename without `.jsonl`) |
| `uuid` | string | Lowercase UUID v4 matching `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$` |
| `cwd` | string | Absolute path of the working directory at session start |

All three fields must be non-empty on every line. The `session_id` and `cwd` values are consistent across all lines within one session run.

## Transcript Location

```
.jenny/sessions/<session_id>/transcript.jsonl
```

Each line is one JSON object. The session directory is created on first write.

## Chain Participants vs Non-Chain Entries

Not every persisted line becomes an API message on reload.

### Chain participants (rebuild conversation)

| `type` | Role in chain |
|--------|---------------|
| `user` | User turn |
| `assistant` | Model turn (may include `tool_use`) |
| `attachment` | Attachment context |
| `system` | Selected system subtypes (e.g. compact boundary) |

### Non-chain entries (persist but do not fork conversation)

| Category | Examples | Behavior on reload |
|----------|----------|-------------------|
| Progress / ephemeral | `progress`, `bash_progress`, `powershell_progress`, `mcp_progress` | UI/telemetry only; skipped for API chain |
| Metadata | `queue-operation`, `custom-title`, `tag`, `file-history-snapshot`, `content-replacement` | Stored; not sent to model |

**Critical:** Progress messages must never become chain nodes. Reloading a transcript with progress entries must not fork or duplicate the conversation.

Legacy transcripts may contain `type: "progress"` entries; on load, rewire `parentUuid` to the nearest chain participant.

## Write Path

1. Append each turn synchronously after it completes (no write buffer).
2. Use append mode for crash recovery (partial last line may be discarded on parse).
3. `FlushPendingWrites()` is a no-op hook (writes are already synchronous).

## Persistence Disable

When persistence is disabled (e.g. `--no-session-persistence`):

- Skip all transcript writes.
- Resume (`-r`) must fail or start fresh (no read from disk).
- Headless print mode only in reference behavior.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Malformed JSONL line | Skip line; log warning; continue parsing |
| Multi-GB transcript | Append-only; no in-place rewrite |
| Process killed mid-write | Resume from last complete lines; skip partial tail |
| `sessionProjectDir` vs cwd drift | Resolve project dir consistently at save and load |
| Concurrent sessions same ID | Last writer wins; document as undefined (single process assumed) |

## Headless Protocol Compatibility

- `session_id` in stream-json output must match the transcript filename stem exactly.
- Terminal `result` line must include the same `session_id` as `system`/`init`.

## Acceptance Criteria

- **AC1:** Chain rebuild includes only user/assistant/attachment/system participants; progress types excluded.
- **AC3:** Shutdown completes without data loss when persistence enabled (synchronous writes).
- **AC4:** With persistence disabled, no files written under `.jenny/sessions/`.
- **AC5:** Append-only writes survive normal crash (at most one partial line lost).
- **AC6:** The `session_id` emitted in the stream-json `system` event and `result` event equals the stem of the `.jsonl` transcript file created in the same run.
- **AC7:** Every transcript line has a non-empty `cwd` field equal to the absolute path of the directory from which jenny was invoked.

## Session Listing

The `Manager.ListSessions()` method returns session IDs sorted by modification time (most recent first). It is used by:
- The `--continue` CLI flag to find the most recent session.
- The Portal's `GET /api/sessions` endpoint.

### Behavior

1. Scans the `sessions/` subdirectory (under the configured jenny dir).
2. Skips non-directory entries.
3. For each subdirectory, checks for the existence of `transcript.jsonl`.
4. Directories without `transcript.jsonl` are silently skipped.
5. Results are sorted by `transcript.jsonl` modification time, descending (most recent first).
6. Returns `nil` (not an empty slice) when no sessions exist or directory is absent.
7. Thread-safe: holds a read lock during directory scan.

### Acceptance Criteria

- **AC8:** `ListSessions` returns sessions sorted by most recent `transcript.jsonl` mtime first.
- **AC9:** Empty `sessions/` directory returns `nil`.
- **AC10:** Non-existent `sessions/` directory returns `nil` (no error).
- **AC11:** Directories without `transcript.jsonl` are excluded from results.
- **AC12:** Non-directory entries in `sessions/` are ignored.

### Related

- Resume behavior: [`session-resume.md`](./session-resume.md)
- Cost restore: [`cost-tracking.md`](./cost-tracking.md)
