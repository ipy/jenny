---
title: Skills Framework
slug: skills-framework
priority: P4
status: done
spec: partial
code: done
package: internal/skills, internal/tool, cmd/jenny
gaps:
  - Bundled skills discovery not implemented
depends_on:
  - read
  - write
  - edit
  - skill
  - tool-registry
  - context-compaction
---
# Skills Framework

## Overview

Discovers and activates skills from project and user directories on file tool operations. This doc covers the path-triggered activation mechanism. For skill definition format, the `activate_skill` tool, and system prompt manifest, see `skill.md`.

## Discovery Triggers

On Read, Write, Edit path access:

- `SkillActivator.ActivateForPath(path)` — checks if any skill matches the accessed path. Returns the names of activated skills, or nil if none match.
- Matching is done via `Skill.MatchesPath()`: either path-within-root or `activation_glob` pattern.

## Path Activation Lifecycle

1. **Registration:** `PathSkillActivator.RegisterActivation(name, rootPath, allowedTools)` tracks activated skills with deduplication.
2. **Tool Wiring:** `WithSkillsFrameworkEnabled(enabled, skills)` on the `Registry` wires `PathSkillActivator` into Read, Write, Edit, NotebookEdit, and Agent tools via `WithSkillActivator`.
3. **Session State:** `GetActivatedSkills()` returns `[]ActivatedSkill` (Name, RootPath, AllowedTools). `GetActivatedTools()` returns the union of all activated skills' allowed tools.
4. **Engine Sync:** After each tool execution, the engine type-asserts for `GetActivatedSkills()` and syncs into `StreamConfig.ActiveSkills`.
5. **Compaction Survival:** `ActiveSkills` and `DiscoveredSkillNames` in `StreamConfig` survive context compaction.

## Sources

- Project `.jenny/skills/` and `.agents/skills/` (highest priority)
- User config skills directory (`~/.jenny/skills/`, `~/.agents/skills/`)
- Plugin skills (lower priority, via `discoverAndMergePluginSkills()`)

Conditional skills activate on path glob match.

## Restrictions

- MCP prompts not invokable as skills (enforced at `tool.Registry` layer)
- `--bare` mode skips discovery (enforced in `cmd/jenny/main.go`)

## Acceptance Criteria

- **AC1:** Skills discovered on Read/Write/Edit paths.
- **AC2:** Conditional activation on glob match.
- **AC3:** MCP prompts not invokable as skills.
- **AC4:** Bare mode skips discovery.
- **AC5:** Activated skills survive context compaction via StreamConfig.
