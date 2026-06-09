## Non-negotiable rules

### Document-driven (mandatory order)

Every behavior change MUST follow this sequence — **never skip or reorder**:

1. **Documentation** — update or add spec under `docs/` (source of truth)
2. **Tests** — unit and integration tests (`*_test.go`) that encode acceptance criteria
3. **Code** — implementation that matches the spec and tests

If requirements are ambiguous, update the doc first; do not guess in code.

### Guideline

The system is designed to be operated by AI agents. Clear file contracts, structured logs, deterministic state transitions.
Enforce minimal tech debt. Fewer, well-chosen dependencies. Delete code aggressively. Lowest abstract complexity.

### Completed Refactoring (iteration 148)

Technical debt refactoring completed for `internal/agent/` and `internal/tool/`:

| AC | Change | Tolerance |
|----|--------|-----------|
| AC1 | Engine.go split into engine.go (267), engine_loop.go (1033), engine_stream.go (248) | ±30 lines of original 1521 |
| AC2 | Iteration test files consolidated into feature-aligned files | — |
| AC3 | captureStdout deduplicated into testhelpers_test.go | — |
| AC4 | makeMockStreamServer* variants consolidated into testhelpers_test.go | — |
| AC5 | TimestampNow() inlined in engine.go | — |
| AC6 | Cost.go split into cost.go + cost_persistence.go | — |
| AC7 | Overlapping normalize tests deduplicated | — |
| AC8 | go vet ./... && go test ./... exit 0 | — |
