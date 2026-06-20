---
title: Skill Tool
slug: skill
priority: P3
status: done
spec: partial
code: done
package: internal/tool, internal/skills
implemented:
  - "Skill discovery from project, user, and bundled directories"
  - "SKILL.md frontmatter parsing (description, activation_glob, allowed_tools)"
  - "activate_skill tool returns root_path and content"
  - "System prompt skills manifest (name + description per skill)"
  - "Active Skills tracked in StreamConfig, communicated via virtual user messages"
  - "Path-based automatic skill activation (ActivationGlob)"
  - "allowed_tools parsed and included in activation response (soft enforcement)"
  - "Hard subagent tool filtering via AllowedTools intersection"
  - "Case-insensitive skill name lookup"
  - "Deduplication across discovery directories"
  - "DiscoveredSkillNames cross-turn persistence"
gaps:
  - SKILL_ROOT env var is advisory only (communicated to model, not programmatically injected)
depends_on:
  - tool-registry
  - subagent-types
  - system-prompt
  - active-skills-persistence
---
# Skill Tool

## Overview

Invokes slash-command skills by name. Skills are portable units of specialized knowledge, instructions, and resources following the [Agent Skills](https://agentskills.io) specification.

## Standard Specification

A skill is a directory containing a mandatory `SKILL.md` file and optional subdirectories.

### Directory Structure
```text
skill-name/
├── SKILL.md       # Required: Metadata + Instructions
├── scripts/       # Optional: Executable code (bash, python, etc.)
├── references/    # Optional: Documentation (PDFs, MD, JSON)
└── assets/        # Optional: Templates, images, or static resources
```

## Behavior & Implementation

### 1. Discovery & Manifest
Skills are discovered at startup from project, user, and bundled directories.
- **System Prompt Manifest:** The `ToolRegistry` populates a "Skills Manifest" section in the system prompt (`system-prompt.md`).
- **Manifest Content:** For each skill, only `name` and `description` are included. This follows the ~100 token "Discovery" requirement.
- **Configuration:** `Registry.WithSkillsFrameworkEnabled(enabled, skills)` wires path-triggered skill activation into Read/Write/Edit tools and creates a `PathSkillActivator`. `Registry.WithSkills(skills)` provides skills without path-triggered activation.

### 2. Activation Tool (`activate_skill`)
The agent invokes `activate_skill(name: string)` to load a skill.

The response is returned as XML-wrapped text:
```xml
<activated_skill root_path="/absolute/path/to/skill">
... full SKILL.md text ...
</activated_skill>
```

### 3. Resource Access & Path Resolution
Skills use relative paths (e.g., `scripts/deploy.sh`).
- **Resolution:** The agent MUST combine the `root_path` with the relative path to use system tools like `Read` or `Bash`.
- **Environment:** The tool's description instructs the agent to set `SKILL_ROOT` to the skill's root path. This is advisory (communicated to the model), not programmatically injected.

### 4. Tracking Active Skills
Active skills are tracked in `StreamConfig.ActiveSkills` and survive context compaction:
- Active skills are communicated via virtual user messages (`<system-reminder>`), not the system prompt. This preserves prompt cache integrity.
- `DiscoveredSkillNames` provides thread-safe cross-turn persistence of skill names, surviving context compaction.
- The engine syncs activated skills from the `SkillActivator` after each tool execution via the optional `GetActivatedSkills()` interface on the concrete `PathSkillActivator`.

### 5. Heavy Skills & Subagents
"Heavy" skills (complex multi-step tasks) should be forked using the `agent` tool (legacy alias: `task`).
- The `agent` tool prompt should include the skill's instructions.
- The `subagent_type` is chosen by the agent (e.g., `explore`, `shell`).

### 6. Security (`allowed_tools`)
- **Format:** Space-separated list of tool patterns in SKILL.md frontmatter (e.g., `Read`, `Bash(git:*)`, `Glob`).
- **Example frontmatter:**
  ```yaml
  allowed_tools: "Read Glob Grep Bash(git:*)"
  ```
- **Enforcement:**
  - **Soft (Implemented):** Parsed from frontmatter and included as `allowed_tools` attribute in the `<activated_skill>` wrapper. The agent is instructed to respect this constraint.
  - **Hard (Implemented):** `SkillActivator.GetActivatedTools()` is passed as `AllowedTools` in `SubagentParams`. The subagent's tool list is intersected with the skill's allowed tools via `SubagentParams.AllowedTools` intersection in `subagent.go`.

## Acceptance Criteria

- **AC1:** `activate_skill` returns `root_path` and content in XML wrapper.
- **AC2:** System prompt contains a manifest of available skill names and descriptions.
- **AC3:** Active skills tracked in `StreamConfig.ActiveSkills`, communicated via virtual user messages.
- **AC4:** Relative paths in skills are resolvable via `root_path`.
- **AC5:** `SKILL_ROOT` env var is advisory (communicated to model via tool description).
- **AC6:** Unknown skill names return a clear error.
- **AC7:** Hard subagent tool filtering via AllowedTools intersection.
