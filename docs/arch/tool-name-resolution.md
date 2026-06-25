---
title: Tool Name Resolution
slug: tool-name-resolution
priority: P2
status: done
spec: complete
code: done
package: internal/tool, internal/agent
depends_on:
  - tool-registry
---
# Tool Name Resolution

## Overview

Models sometimes invoke tools using names that do not exactly match the registered tool list — wrong casing, informal synonyms, or hyphenated variants. Before execution, the agent resolves the requested name against the available tools using a fixed strategy chain. Resolution is server-side only; the tool list sent to the API is unchanged.

## Resolution strategies

Given a requested tool name and the current available tool set, resolution tries strategies in order until a registered name matches:

| Order | Strategy | Example |
|-------|----------|---------|
| 1 | Exact match | `Read` → `Read` |
| 2 | Case-insensitive match | `read` → `Read`, `BASH` → `Bash` |
| 3 | Alias table lookup | `file_reader` → `Read`, `terminal` → `Bash`, `task` → `agent` |
| 4 | Hyphen/underscore normalization, then retry 1–3 | `web-fetch` → `web_fetch`, `call-mcp` → `call_mcp` |

Case-insensitive matching uses Unicode-aware folding (`strings.EqualFold`).

When strategy 1 succeeds, resolution is considered a direct hit (no alias applied). Any match via strategies 2–4 is treated as resolved.

## Alias table

Aliases live in the tool package as an extensible map. Initial entries:

| Alias | Canonical name |
|-------|------------------|
| `file_reader` | `Read` |
| `terminal` | `Bash` |
| `shell` | `Bash` |
| `search` | `Grep` |
| `find_files` | `Glob` |
| `write_file` | `write` |
| `edit_file` | `edit` |
| `task` | `agent` |

Alias keys are matched case-insensitively. The canonical name must exist in the available tool set (exact or case-insensitive) or resolution continues to the next strategy.

## Registry lookup

Exact-match lookup by name remains unchanged for callers that need it. Resolution is a separate step: resolve the name, then look up the tool by the resolved name.

## Executor integration

During tool batch partitioning, each tool-use block name passes through resolution before lookup. When resolution applies a non-exact match, a debug log records the attempted and resolved names.

Partitioning and execution semantics (parallel vs serial, sibling abort) continue to use the original block name where name-based heuristics already apply (e.g. shell vs read-only classification via case-insensitive checks).

## Unknown tool error

When no strategy yields a registered tool, the executor returns an immediate synthetic error (never hangs). The message includes:

- The attempted tool name
- A comma-separated list of available tool names
- A closest-match suggestion derived from edit distance among available names

Example shape:

```
Error: No such tool available: UnknownTool
Available tools: Read, Bash, Glob, Grep
Did you mean: Glob?
```

## Constraints

- Resolution does not mutate the API tool list or affect prompt cache stability.
- Aliases are not duplicated in the agent package; the tool package owns the alias table.
