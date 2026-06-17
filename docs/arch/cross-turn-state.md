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
QueryEngine exposes a method to set the maximum budget in USD. When set, the engine checks accumulated cost against this budget before each API call. If exceeded, the loop terminates with result error type `error_budget_exceeded`. The method is the canonical setter for the existing StreamConfig field.

### AC2: permissionDenials cross-turn persistence
When a tool execution is denied by the permission gate, the denial reason (tool name + input summary) is recorded in StreamConfig. On subsequent turns within the same query, a tool with identical name and input summary is not re-executed — the engine returns the cached denial. Permission denials survive context compaction as a non-compacted field.

### AC3: discoveredSkillNames cross-turn persistence
Skill names discovered during execution (e.g., via path-matching in skills framework) are stored in StreamConfig across turns within the same query. This field is non-compacted and survives compaction. A skill name is only appended if not already present (deduplication).

### AC4: No regression on existing ACs
All existing QueryEngine acceptance criteria (AC1-AC5 from query-engine.md pass, all existing Active Skills Persistence ACs pass, all existing tool tests pass).

### AC5: Graceful nil-budget degradation
When maxBudgetUsd is 0 (unset), cost checking is skipped entirely — no error, no early termination, no performance overhead.

## Implementation Architecture

### Non-compacted State Location
Permission denials and discovered skill names are stored as sibling fields alongside `ActiveSkills` in StreamConfig. All are marked as non-compacted fields that survive context compaction.

### Budget Method
QueryEngine exposes a setter method for the maximum budget. The cost check runs in the engine loop before each API call — when a budget is set and accumulated cost exceeds it, the loop terminates early.

### Permission Denial Hook Point
The permission gate is called during tool execution. After a denial, the reason is appended to the denial list. Before executing a tool, the denial list is checked for a matching entry. Match on tool name and serialized input keys (keys sorted alphabetically for deterministic matching).

### Discovered Skill Names Hook Point
Skill names discovered during tool execution are accumulated in StreamConfig with deduplication before appending.

### Thread Safety
All concurrent access to StreamConfig denial and skill name lists from parallel tool execution goroutines is protected by a mutex embedded in StreamConfig.

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
