---
title: Koanf Configuration Layering
slug: koanf-config
priority: P4
status: done
spec: complete
code: done
package: internal/cli
gaps: []
depends_on:
  - cli
---
# Koanf Configuration Layering

## Overview

Migrate CLI flag parsing from the standard library `flag` package to `koanf` with `pflag`, enabling layered configuration from JSON config files, environment variables, and CLI flags.

## Configuration Sources (Highest to Lowest Precedence)

1. **CLI Flags**: Command-line arguments via `pflag`
2. **Environment Variables**: Prefixed with `JENNY_` (e.g., `JENNY_MODEL`, `JENNY_OUTPUT_FORMAT`)
3. **JSON Configuration**: `.jenny/config.json` in the current working directory

## Loading Order (Lowest to Highest Precedence)

1. JSON File: `k.Load(file.Provider(".jenny/config.json"), json.Parser())` — ignore error if file missing
2. Environment Variables: `k.Load(env.Provider("JENNY_", ".", transformFunc), nil)` — transform strips `JENNY_` prefix, lowercases, replaces `_` with `-`
3. CLI Flags: `k.Load(posflag.Provider(flags, ".", k), nil)`

## Struct Tags

The `Flags` struct uses `koanf:"<key>"` tags for unmarshalling:

- `koanf:"model"` for Model
- `koanf:"output-format"` for OutputFormat
- `koanf:"mcp-config"` for MCPConfig (string slice)
- `koanf:"resume"` for SessionResume
- `koanf:"deny-tool"` for DeniedTools (string slice)
- `koanf:"permission-level"` for PermissionLevel

## Environment Variable Mapping

| Variable | Config Key | Type | Description |
|----------|-----------|------|-------------|
| `JENNY_MODEL` | `model` | string | Default model |
| `JENNY_OUTPUT_FORMAT` | `output-format` | string | `text` or `stream-json` |
| `JENNY_VERBOSE` | `verbose` | bool | Debug-level logging |
| `JENNY_MCP_CONFIG` | `mcp-config` | []string | MCP config file paths |
| `JENNY_PERMISSION_LEVEL` | `permission-level` | string | `read`/`analyze`/`edit`/`execute`/`unrestricted` |
| `JENNY_REDACT` | `redact` | string | `disabled`/`redact` (default)/`recover` |
| `JENNY_TRANSCRIPT_DIR` | `transcript-dir` | string | Override transcript directory |
| `JENNY_MAX_TOOL_CONCURRENCY` | `max-tool-concurrency` | int | Max parallel tool executions (0 = default 10) |
| `JENNY_COMPACT_KEEP_ARCHIVE` | `compact-keep-archive` | bool | Keep `<id>.tar.gz` after resume extraction |
| `JENNY_DISABLE_COMPACT` | `disable-compact` | bool | Disable all compaction (manual + auto) |
| `JENNY_DISABLE_AUTO_COMPACT` | `disable-auto-compact` | bool | Disable auto-compact only |
| `JENNY_ENABLE_SESSION_MEMORY` | `enable-session-memory` | bool | Enable session-memory compaction branch |
| `JENNY_DISABLE_AUTO_MEMORY` | `disable-auto-memory` | bool | Disable auto-memory directory entirely |

### Precedence Summary

For any given config key:

1. CLI flag (highest) — e.g. `--redact-mode=disabled`
2. `JENNY_*` env var — e.g. `JENNY_REDACT=disabled`
3. `.jenny/config.json` field — e.g. `{"redact": "disabled"}`
4. Built-in default (lowest) — e.g. `redact` (one-way mode)

## Out of Koanf Layer

A small number of environment variables are intentionally read directly via `os.Getenv` rather than going through the koanf layer, because their consumers run before `cli.Parse()` is reachable or they follow third-party SDK conventions:

- `JENNY_DEBUG`, `JENNY_VERBOSE`, `DEBUG` — read in `internal/log/log.go:32` from `init()` (pre-`cli.Parse`); the canonical site is the log package.
- `JENNY_HOME`, `JENNY_AGENTS_HOME` — read in `internal/constants/constants.go` from `init()`; controls the jenny home directory.
- `ANTHROPIC_*`, `OPENAI_*`, `GENAI_*`, `GOOGLE_*`, `GEMINI_*` — third-party SDK / proxy conventions; not Jenny's to namespace.
- `MCP_HTTP_REQUEST_TIMEOUT`, `MCP_MAX_OUTPUT_CHARS` — MCP-layer knobs; kept in-package.
- `AUTO_COMPACT_WINDOW` — non-`JENNY_*` prefix; legacy override for `CompactConfig.ModelContextWindow`. Coexists with the migrated C-group vars inside `newCompactConfigForModel`.
- `TEMP`, `TMP`, `LOCALAPPDATA`, `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `API_TIMEOUT_MS` — OS / standard HTTP / library conventions.

## Acceptance Criteria

- **AC1:** Koanf dependencies added (koanf/v2, pflag, env/file/posflag providers, json parser). YAML parser NOT imported.
- **AC2:** Pflag replaces stdlib `flag`. All aliases and defaults retained. Mutual exclusion checks still functional.
- **AC3:** Layered configuration with correct precedence (CLI > ENV > JSON).
- **AC4:** `Flags` struct has `koanf:"<key>"` tags and unmarshalls via `koanf.Unmarshal`.
- **AC5:** All tests updated to POSIX double-dash syntax and passing.

## Out of Scope

- YAML support
- System-wide/global configs (`~/.jenny/config.json`)
- Removing existing `.env` loading
- Changes to agent runtime logic

## Related

- Permission levels: [permission-levels.md](../patterns/permission-levels.md)
