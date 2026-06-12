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

The Active Skills feature tracks activated skills across the session lifecycle and injects them into the system prompt's dynamic suffix, ensuring the model always knows which skills are actively in use, even after compaction strips prior tool call history.

## Acceptance Criteria

### AC1: Explicit activation via ActivateSkill tool
Calling `activate_skill` with a valid skill name calls `RegisterActivation` on the `SkillActivator`, recording the skill name and root path. The tool returns skill content wrapped in `<activated_skill root_path="...">` tags.

### AC2: Implicit path-triggered activation
Reading, writing, or editing a file whose path matches a skill's `activation_glob` calls `ActivateForPath`, which calls `RegisterActivation` for each matching skill. The activator deduplicates so the same skill is not double-counted.

### AC3: syncActiveSkills wiring
After every tool execution iteration in the engine loop, `syncActiveSkills()` is called. This method converts `skills.ActivatedSkill` entries to `agent.ActivatedSkill` entries and stores them in `StreamConfig.ActiveSkills`. If the activator is nil, this is a no-op.

### AC4: Active skills in dynamic suffix
`DynamicSystemSuffix()` includes an "Active Skills:" section listing all active skills with their name and root path. This section is NOT part of the cached prefix, so it updates every turn without busting prompt caching for the stable ~1000+ token prefix.

### AC5: Compaction survival
`ActiveSkills []ActivatedSkill` is a field on `StreamConfig`, which is non-compacted state that persists across compaction boundaries. Skills activated before compaction remain visible in the system prompt after compaction.

### AC6: Deduplication
Activating the same skill twice (e.g., reading two files matching the same skill's glob) does not create duplicate entries. `RegisterActivation` checks if the skill name is already registered before appending.

### AC7: Graceful nil-activator degradation
When `skillActivator` is nil (bare mode or no skills framework), `syncActiveSkills` returns immediately without panic. The system prompt suffix contains no active skills section.

### AC8: Main.go wiring
Production entry point at `cmd/jenny/main.go` retrieves the skill activator from the registry via `GetSkillActivator()` and passes it to `RunStream` via `agent.WithSkillActivator(skillActivator)` as a `QueryEngineOption`.

### AC9: End-to-end tool-to-prompt propagation
A skill activated via the `activate_skill` tool during a conversation turn is reflected in the system prompt suffix of the next API request, verified by integration tests that mock the activator chain.

## Implementation Architecture

### Type Bridge
`skills.ActivatedSkill` and `agent.ActivatedSkill` are structurally identical but distinct Go types:

```go
// skills/skill_activator.go
type ActivatedSkill struct {
    Name     string
    RootPath string
}

// agent/stream_types.go
type ActivatedSkill struct {
    Name     string
    RootPath string
}
```

`syncActiveSkills()` converts between them via a manual copy loop (engine.go lines 377-394).

### Prompt Architecture
Active skills live in `DynamicSystemSuffix()` (prompt.go lines 251-255, via `activeSkillsSection`). This avoids breaking the stable cached prefix which includes intro, memory, tool list, skills manifest, and redaction instruction.

### syncActiveSkills Call Site
`internal/agent/engine_loop.go` lines 702-704, immediately after tool execution completes. This runs every loop iteration, picking up both explicit (tool call) and implicit (path-triggered) activations.

### Registry Wiring
`WithSkillsFrameworkEnabled` creates a `PathSkillActivator` and stores it in `Registry.skillActivator`. `Registry.Build()` wires it into Read/Write/Edit tools (lines 269-279) and into the SkillTool (lines 331-336). The activator is exposed via `GetSkillActivator()` for the main.go engine wiring.

## Out of Scope

- Skill editing, creation, or management after session start (skills are discovered at startup only).
- Cross-session persistence of active skills (active skills are session-scoped and reset on new sessions).
- UI or CLI-level feedback for activation events (stream-json events were implemented separately in a prior iteration).
- Timing/ordering guarantees between explicit and implicit activation paths (both converge on the same `RegisterActivation`).
- Swarm/subagent active skills inheritance or sharing across forked sessions.
- Performance optimization of the dynamic suffix for very large active skills lists (expected O(1-5) in practice).

## Test Coverage

- `TestAC1_SkillActivatorWiring`: Verifies skill activator wiring and type conversion
- `TestAC1_SkillActivatorDeduplication`: Verifies duplicate activations are not added
- `TestAC1_SkillActivatorNoOpWhenNil`: Verifies syncActiveSkills is no-op when activator is nil
- `TestActiveSkillsSection_Empty`: No Active Skills section when no skills are active
- `TestActiveSkillsSection_WithSkills`: Active Skills section appears when skills are activated
- `TestActiveSkillsSection_MultipleSkills`: Multiple skills are all shown
- `TestActiveSkillsSection_Format`: Exact format of the Active Skills section
- `TestActiveSkillsSection_CacheFriendly`: Dynamic suffix changes when active skills change
- `TestActiveSkillsSection_CompactionSurvival`: Active Skills survive context compaction
- `TestSetActiveSkills`: SetActiveSkills correctly updates the ActiveSkills field
