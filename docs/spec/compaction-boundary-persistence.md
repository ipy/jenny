# Compaction Boundary Persistence & Session Resume Filtering

## Motivation

Context compaction summarizes older conversation turns to fit within the model context window. However, after compaction, the `buildCompactedChain` function inserts a `system`-role boundary message into the in-memory API chain but never persists it to the JSONL transcript. When a user later resumes the session with `-r`, `LoadTranscript` replays all entries including pre-boundary messages, sending the full uncompacted history to the API — defeating the purpose of compaction and wasting tokens on already-summarized content.

This iteration wires compaction boundaries through the persistence layer so that resumed sessions after compaction correctly omit pre-boundary messages from the API payload, preserving the token savings.

## Numbered Testable ACs

### AC1 — Compaction boundary persisted to transcript as `system`/`compact_boundary` entry
After `buildCompactedChain` produces a compacted chain, the engine persists a transcript entry with:
```json
{"type":"system","content":"","subtype":"compact_boundary","compact_metadata":{"trigger":"auto","pre_tokens":<int>,"preserved_segment":<int>}}
```
Verified by calling the persistence path after compaction and asserting the transcript file contains a JSONL line with `"subtype":"compact_boundary"`.

### AC2 — `LoadTranscript` detects compaction boundary and returns only post-boundary entries when asked
Given a transcript with entries before and after a `compact_boundary` entry:
- `LoadTranscript(sessionID)` behavior is unchanged (returns all entries for stream-json replay)
- A new method `LoadPostBoundaryMessages(sessionID)` returns only entries after the last `compact_boundary`
Verified by `go test ./internal/session/ -count=1 -run TestAC2_LoadPostBoundaryMessages`.

### AC3 — Resume after compaction sends only post-boundary messages to API
When `-r <session_id>` is used on a session that has undergone compaction, the resumed message chain sent to the API contains only the boundary marker + post-boundary messages. Pre-boundary entries are excluded.
Verified by an engine-level test that sets up a compacted transcript, resumes, and inspects the assembled API message chain.

### AC4 — Compaction boundary metadata includes trigger, pre_tokens, and preserved_segment
Each `compact_boundary` entry includes `compact_metadata` with:
- `trigger`: one of `"auto"`, `"manual"`
- `pre_tokens`: token count before compaction
- `preserved_segment`: count of messages preserved

Verified by asserting the metadata fields are non-zero in the persisted entry.

### AC5 — Multiple compactions: only the most recent boundary applies
If a session undergoes compaction twice (two boundary markers), `LoadPostBoundaryMessages` returns only entries after the *latest* boundary. Earlier boundaries and the messages between them are excluded.
Verified by appending multiple boundaries to a test transcript and asserting only post-last-boundary entries are returned.

### AC6 — No regression: existing resume tests pass
All existing session resume tests pass with `go test ./internal/agent/ ./internal/session/ ./cmd/jenny/ -count=1 -run TestResume`.

## Implementation Details

### 1. Transcript Entry Schema Extension
Add to `TranscriptEntry` in `internal/session/manager.go`:
```go
Subtype          string           `json:"subtype,omitempty"`
CompactMetadata  *CompactMetadata `json:"compact_metadata,omitempty"`
```

Add new struct:
```go
type CompactMetadata struct {
    Trigger string `json:"trigger"`
    PreTokens        int    `json:"pre_tokens"`
    PreservedSegment int    `json:"preserved_segment"`
}
```

### 2. Persistence Hook Location
In `internal/agent/engine_loop.go` after `e.compactMessages(...)` returns successfully:
```go
e.persistCompactBoundary(preTokens int, preservedCount int)
```

This method writes a `system`/`compact_boundary` entry via `e.sessionManager.AppendEntry`.

### 3. New Session Method
Add `LoadPostBoundaryMessages(sessionID string) ([]TranscriptEntry, error)` to `internal/session/manager.go`:
- Scans transcript for the last entry where `entry.Type == "system" && entry.Subtype == "compact_boundary"`
- Returns all entries after that line
- If no boundary exists, returns all entries (current `LoadTranscript` behavior)

### 4. Resume Wiring
In `cmd/jenny/main.go`, after loading transcript:
- Call `LoadPostBoundaryMessages` instead of `LoadTranscript` when building history
- This ensures only post-boundary messages are sent to the API

### 5. Boundary Marker Content
`buildCompactedChain` already produces `system` content `"[Context boundary: ...]"`. This is the in-memory marker for the API chain. The persisted `compact_boundary` entry is separate (goes to transcript only).

## Out of Scope

- Session memory compaction (P3, `trySessionMemoryCompact` currently returns `ErrNotImplemented`)
- Natural break detection for session memory updates
- Any changes to MCP client, read tool image/PDF support, or skill `allowed-tools` enforcement
- Any changes to the compact summary agent logic, circuit breaker, or threshold math
- Windows native support
- Deep-copy or thread-safety changes outside session/agent packages
- Any changes to `cmd/jenny/main.go` beyond resume wiring (no CLI flag additions or removals)
