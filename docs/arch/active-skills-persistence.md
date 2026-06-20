---
title: Active Skills Persistence
slug: active-skills-persistence
priority: P1
status: done
spec: complete
code: done
package: internal/agent, internal/skills, internal/tool
gaps: []
depends_on:
  - skill-registry
---

# Active Skills Persistence — System Prompt Integration

## Motivation

The skills framework allows activating skills either explicitly via the ActivateSkill tool or implicitly via path-based activation on file reads/writes/edits. However, once a skill was activated, there was no mechanism to communicate which skills were active in the system prompt. This meant the model had no awareness of active skills between turns, especially after context compaction which could drop prior skill activation messages from the conversation history.

The Active Skills feature tracks activated skills across the session lifecycle and injects them as virtual user messages in the message chain, ensuring the model always knows which skills are actively in use, even after compaction strips prior tool call history.

## Acceptance Criteria

### AC1: Explicit activation via ActivateSkill tool
Calling `activate_skill` with a valid skill name calls `RegisterActivation` on the `SkillActivator`, recording the skill name, root path, and allowed tools. The tool returns skill content wrapped in `<activated_skill root_path="..." allowed_tools="...">` tags (the `allowed_tools` attribute is included only when the skill defines allowed tools).

### AC2: Implicit path-triggered activation
Reading, writing, or editing a file whose path matches a skill's `activation_glob` calls `ActivateForPath`, which calls `RegisterActivation` for each matching skill. The activator deduplicates so the same skill is not double-counted.

### AC3: syncActiveSkills wiring
Active skills are synchronized at two points in the engine loop:
1. **Inner sync** (`executeAndProcessTools`): runs after tool execution completes, picking up tool-triggered activations immediately.
2. **Outer sync** (loop level): runs after all tool results are collected. If the active skill count changed, a `<system-reminder>` virtual user message is injected as a cross-turn reminder so the model sees updated skills in the next API request.

Skills framework activation entries are converted to agent-level entries and stored in StreamConfig. If the activator is nil, both sync points are a no-op.

### AC4: Active skills in message chain
Active skills are communicated to the model via `<system-reminder>` virtual user messages injected into the message chain (not the system prompt). This is done at two injection points:
1. **Compaction re-injection**: after context compaction, a virtual user message is appended so the model retains awareness of active skills even when prior activation tool results are summarized away.
2. **Cross-turn change detection**: at the loop level (AC3 outer sync), when the active skill count changes, a virtual user message is appended.

`DynamicSystemSuffix()` is intentionally empty — all dynamic content uses message-chain injection to keep the system prompt prefix byte-stable across turns, preventing cache invalidation.

### AC5: Compaction survival
`ActiveSkills []ActivatedSkill` is a field on `StreamConfig`, which is non-compacted state that persists across compaction boundaries. Skills activated before compaction remain visible in the system prompt after compaction.

### AC6: Deduplication
Activating the same skill twice (e.g., reading two files matching the same skill's glob) does not create duplicate entries. `RegisterActivation` checks if the skill name is already registered before appending.

### AC7: Graceful nil-activator degradation
When `skillActivator` is nil (bare mode or no skills framework), `syncActiveSkills` returns immediately without panic. No active skills virtual messages are injected.

### AC8: Main entry point wiring
The production entry point retrieves the skill activator from the registry and passes it to the stream runner as a query engine option.

### AC9: End-to-end tool-to-prompt propagation
A skill activated via the `activate_skill` tool during a conversation turn is reflected in the message chain of the next API request (via virtual user message injection), verified by integration tests that mock the activator chain.

## Implementation Architecture

### Type Bridge
The skills framework and the agent engine use structurally identical but distinct types for activated skills (`skills.ActivatedSkill` and `agent.ActivatedSkill`), both carrying `Name`, `RootPath`, and `AllowedTools []string`. A conversion step bridges between them at each sync point. The `SkillActivator` interface exposes `GetActivatedTools()` which returns the union of allowed tools across all active skills.

### Prompt Architecture
Active skills are injected as virtual user messages in the message chain, not in the system prompt. `DynamicSystemSuffix()` is intentionally empty. This keeps the system prompt prefix byte-stable (intro, memory, tool list, skills manifest, redaction instruction) and avoids cache invalidation when active skills change.

### Active Skills Sync Point
Active skills are synchronized at two points per loop iteration:
1. **Inner** (inside `executeAndProcessTools`): picks up both explicit (tool call) and implicit (path-triggered) activations immediately after tool execution.
2. **Outer** (loop level): detects cross-turn changes and injects a `<system-reminder>` virtual user message reminder when the skill count changes. This ensures the model sees updated skills context even if no new tool calls trigger activation.

### Registry Wiring
When the skills framework is enabled, the registry creates a path-based skill activator and wires it into Read/Write/Edit/NotebookEdit tools and the Skill tool. The activator is exposed for the main entry point engine wiring.

## Out of Scope

- Skill editing, creation, or management after session start (skills are discovered at startup only).
- Cross-session persistence of active skills (active skills are session-scoped and reset on new sessions).
- UI or CLI-level feedback for activation events (stream-json events were implemented separately in a prior iteration).
- Timing/ordering guarantees between explicit and implicit activation paths (both converge on the same `RegisterActivation`).
- Swarm/subagent active skills inheritance or sharing across forked sessions.
- Performance optimization for very large active skills lists (expected O(1-5) in practice).

## Test Coverage

Unit tests (`prompt_active_skills_test.go`):
- `TestActiveSkillsSection_Empty`: No active skills section when no skills are active
- `TestActiveSkillsSection_WithSkills`: Active skills section appears when skills are activated
- `TestActiveSkillsSection_MultipleSkills`: Multiple skills are all shown
- `TestActiveSkillsSection_Format`: Exact format of the active skills section
- `TestDynamicSystemSuffix_AlwaysEmpty`: DynamicSystemSuffix returns empty (active skills use message chain)
- `TestActiveSkills_SurviveCompaction_InStreamConfig`: Active skills persist in StreamConfig across compaction
- `TestSetActiveSkills`: SetActiveSkills correctly updates the ActiveSkills field

E2E tests (`active_skills_e2e_test.go`):
- `TestActiveSkills_E2E_ThroughCompaction`: End-to-end activation through compaction boundary
- `TestActiveSkills_AccumulateAcrossTurns`: Skills accumulate across multiple turns
- `TestCompaction_PreservesNonCompactedFields`: Non-compacted fields survive compaction
- `TestActiveSkills_GracefulDegradation`: Graceful handling when activator is nil
- `TestActiveSkills_NoRegression`: Regression guard for active skills behavior
