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

| Variable | Config Key | Example |
|----------|-----------|---------|
| `JENNY_MODEL` | `model` | `JENNY_MODEL=deepseek-v4-flash` |
| `JENNY_OUTPUT_FORMAT` | `output-format` | `JENNY_OUTPUT_FORMAT=stream-json` |
| `JENNY_VERBOSE` | `verbose` | `JENNY_VERBOSE=true` |
| `JENNY_MCP_CONFIG` | `mcp-config` | JSON array or single path |
| `JENNY_PERMISSION_LEVEL` | `permission-level` | `JENNY_PERMISSION_LEVEL=execute` |

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
