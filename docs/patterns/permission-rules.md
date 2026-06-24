---
title: Permission Rules
slug: permission-rules
priority: P3
status: partial
spec: complete
code: partial
package: internal/tool
gaps: []
depends_on:
  - permission-levels
  - koanf-config
---
# Permission Rules

## Overview

Permission rules provide fine-grained control over which tool invocations are automatically allowed or denied. Rules are loaded from `settings.json` (global or project-level) and evaluated before each tool execution.

Permission rules complement the existing [permission levels](./permission-levels.md) system. Permission levels control what *category* of operations is allowed (read/analyze/edit/execute/unrestricted); permission rules control *specific* tool invocations.

## Configuration

Rules are stored in `settings.json` under the `permissions` key:

```json
{
  "permissions": {
    "allow": ["Read", "Glob", "Grep", "Bash(npm run build)", "Bash(go test:*)"],
    "deny": ["Bash(curl:*)", "Bash(rm -rf:*)", "Write(.env)"]
  }
}
```

Both `allow` and `deny` are string arrays. Each entry follows the pattern syntax below.

## Pattern Syntax

| Pattern | Matches |
|---------|---------|
| `ToolName` | Any invocation of the tool |
| `ToolName(exact)` | Invocation where the primary argument matches exactly |
| `ToolName(prefix:*)` | Invocation where the primary argument starts with `prefix` |

The "primary argument" is tool-specific:

| Tool | Primary argument |
|------|-----------------|
| Bash | The `command` field |
| Read | The `file_path` field |
| Write | The `file_path` field |
| Edit | The `file_path` field |
| Glob | The `pattern` field |
| Grep | The `pattern` field |
| WebFetch | The `url` field |
| WebSearch | The `query` field |
| Task | The `prompt` field |

For tools not listed above, only the bare `ToolName` form matches.

## Evaluation

1. Check deny rules first: if any deny rule matches, the tool invocation is **blocked**.
2. Check allow rules: if any allow rule matches, the tool invocation is **allowed** without prompting.
3. If neither matches, fall through to the default permission level behavior.

Deny rules always take precedence over allow rules.

## CLI Flags

The `--allowedTools` / `--allowed-tools` and `--disallowedTools` / `--disallowed-tools` CLI flags also feed into permission rules:

- `--allowedTools` entries are merged into the `allow` list.
- `--disallowedTools` entries with scoped patterns (e.g. `Bash(rm *)`) are merged into the `deny` list.
- `--disallowedTools` bare names (e.g. `Edit`) remove the tool from the registry entirely (same as `--deny-tool`).

## Loading

Rules are loaded from the koanf config layer (same precedence as other config):

1. `~/.jenny/settings.json` (global)
2. `.jenny/settings.json` (project)
3. `JENNY_*` env vars (not applicable for structured permissions)
4. CLI flags (`--allowedTools` → allow, scoped `--disallowedTools` → deny)

Project rules merge with global rules: project `deny` entries are appended to global `deny` entries; project `allow` entries are appended to global `allow` entries.

## Interaction with Permission Levels

- `--dangerously-skip-permissions` / `--permission-level unrestricted`: permission rules are still evaluated. Deny rules can still block operations.
- Permission rules do not override permission level restrictions. A tool denied by permission level cannot be allowed by a permission rule.
