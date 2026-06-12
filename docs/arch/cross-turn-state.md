---
title: Cross-Turn State Formalization for QueryEngine
slug: cross-turn-state
priority: P1
status: done
spec: complete
code: done
package: internal/agent, internal/tool
gaps: []
depends_on:
  - active-skills-persistence
---

# Cross-Turn State Formalization for QueryEngine

## Motivation

The agent's long-horizon task execution depends on state that accumulates across turns within a single query (SubmitMessage call): permission denials (to avoid retrying the same denied operation), discovered skill names (to avoid re-scanning), and cost budgets (to stop before exceeding limits). Currently, permissionDenials and discoveredSkillNames are tracked ad-hoc in the engine loop without formal persistence guarantees, and maxBudgetUsd is a StreamConfig field but lacks a dedicated QueryEngine method.

The Active Skills Persistence feature established the pattern: non-compacted state on `StreamConfig` survives context compaction and is the canonical place for session-scoped accumulators. This iteration applies the same pattern to formalize cross-turn state: move `permissionDenials` and `discoveredSkillNames` to non-compacted `StreamConfig` fields, and add a `SetMaxBudgetUsd()` method on QueryEngine that gates pre-API cost checks.

## Acceptance Criteria

### AC1: maxBudgetUsd as QueryEngine method
`QueryEngine` exposes `SetMaxBudgetUsd(amount float64)` that sets the field on StreamConfig. When set, the engine checks accumulated cost against this budget before each API call. If exceeded, the loop terminates with result error type `error_budget_exceeded`. The existing `maxBudgetUsd` StreamConfig field is not removed — the method is the canonical setter.

### AC2: permissionDenials cross-turn persistence
When a tool execution is denied by the permission gate, the denial reason (tool name + input summary) is recorded in `StreamConfig.PermissionDenials []string`. On subsequent turns within the same SubmitMessage, a tool with identical name and input summary is not re-executed — the engine returns the cached denial. PermissionDenials is a non-compacted field that survives compaction.

### AC3: discoveredSkillNames cross-turn persistence
Skill names discovered during execution (e.g., via path-matching in skills framework) are stored in `StreamConfig.DiscoveredSkillNames []string` across turns within the same query. This field is non-compacted and survives compaction. A skill name is only appended if not already present (deduplication).

### AC4: No regression on existing ACs
All existing QueryEngine acceptance criteria (AC1-AC5 from query-engine.md pass, all existing Active Skills Persistence ACs pass, all existing tool tests pass).

### AC5: Graceful nil-budget degradation
When maxBudgetUsd is 0 (unset), cost checking is skipped entirely — no error, no early termination, no performance overhead.

## Implementation Architecture

### Non-compacted State Location
`StreamConfig` already has `ActiveSkills []ActivatedSkill`. Added `PermissionDenials []string` and `DiscoveredSkillNames []string` as sibling fields, marked with comments: `// Non-compacted: survives context compaction.`

### maxBudgetUsd Method
Added to `internal/agent/engine.go`. Signature: `func (e *QueryEngine) SetMaxBudgetUsd(amount float64)`. Implementation: `e.streamCfg.MaxBudgetUSD = amount`. The cost check lives in `engine_loop.go` before the API call — gate on `e.streamCfg.MaxBudgetUSD > 0 && totalCost >= e.streamCfg.MaxBudgetUSD`.

### permissionDenials Hook Point
The permission gate is called during tool execution in the executor. After a denial, append to `e.streamCfg.AddPermissionDenial()`. Before executing a tool, check `e.streamCfg.HasPermissionDenial()` for a matching entry. Match on `toolName + toolInputKey` (serialized input keys via `BuildDenialKey()`).

### discoveredSkillNames Hook Point
The skills framework's skill discovery (called during tool execution) already accumulates names. Currently stored locally in the loop. Moved to `e.streamCfg.AddDiscoveredSkillName()` with a helper that deduplicates before appending.

### Thread Safety
`AddPermissionDenial`, `HasPermissionDenial`, and `AddDiscoveredSkillName` use `sync.Mutex` on StreamConfig to protect concurrent access from parallel tool execution goroutines. The mutex is embedded in StreamConfig.

### Denial Key Format
`BuildDenialKey(toolName string, input map[string]any) string` generates a unique key: `toolName:key1=val1,key2=val2` where keys are sorted alphabetically for deterministic matching.

## Test Coverage

- `TestSetMaxBudgetUsd_sets_field`: Method sets StreamConfig field correctly.
- `TestBudgetZero_is_noop`: No budget set; loop runs normally without cost checking overhead.
- `TestPermissionDenial_cached`: Tool denied once; second identical call returns cached denial.
- `TestDiscoveredSkillNames_survives_compaction`: Skill names persist after simulated compaction and deduplicate correctly.
- `TestBuildDenialKey`: Verifies deterministic key generation for tool+input combinations.

## Out of Scope

- readFileState round-trip formalization (requires Read tool state cache — deferred P3, separate iteration).
- Cross-session persistence of permissionDenials or discoveredSkillNames (session-scoped only; new session = empty).
- Full permission system redesign (schema changes, policy engine, role-based access).
- Plugin system integration (depends on mcp-config + mcp-client completion first).
- UI/CLI feedback for permission denials (stream-json events handled separately).
- Performance optimization of denial list lookups (expected O(10) in practice — linear scan is fine).
- Structured output schema validation changes.
- Subagent/swarm cross-turn state inheritance.
