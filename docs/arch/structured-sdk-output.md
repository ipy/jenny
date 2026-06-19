---
title: Structured SDK Output
slug: structured-sdk-output
priority: P4
status: done
spec: partial
code: done
package: internal/agent, internal/tool
gaps: []
depends_on:
  - query-engine
---
# Structured SDK Output

## Overview

Enforces JSON schema output via synthetic StructuredOutput tool in non-interactive sessions.

## Requirements

- JSON schema param on QueryEngine/SDK entry
- Synthetic `StructuredOutput` tool in tool pool
- Stream-json mode enabled (`cfg.Enabled == true`)
- Tool can be excluded via `StructuredDenyRules` deny list; conflict between schema + deny rule fails at engine construction

## Validation

- `ValidateStructuredSchema(schemaStr)` provides pre-validation: parses JSON, checks type is "object" if present, checks properties is object if present. Callers should use this before passing schemas to the engine.
- `validateAgainstSchema` performs basic structural validation (type and required field checks) at tool invocation time — not at registration time.
- `Reset()` clears the `emitted` flag at the start of each turn, allowing exactly one StructuredOutput call per turn in multi-turn sessions.
- Model must emit exactly one StructuredOutput call at end of turn; `IsEmitted()` checked at turn end.

## Subagent Inheritance

`StructuredSchema` and `StructuredDenyRules` propagate from parent to child `StreamConfig` when forking a subagent.

## Acceptance Criteria

- **AC1:** Schema + output tool both required.
- **AC2:** Invalid schema fails at first StructuredOutput tool invocation (not at registration).
- **AC3:** Exactly one structured output call per turn enforced via Reset/IsEmitted.
- **AC4:** Stream-json mode only (cfg.Enabled=true).
