---
title: MCP Configuration
slug: mcp-config
priority: P0
status: draft
spec: partial
code: done
package: internal/mcp
implemented:
  - "CLI flags (--mcp-config, --strict-mcp-config)"
  - "Config file loading with mcpServers wrapper"
  - "Multiple config file merge (later wins)"
  - "Environment variable expansion (${VAR} and ${VAR:-default})"
  - "Strict mode skips plugin sources"
  - "Plugin MCP server discovery and merge"
  - "Server definition supports command/args/env (stdio) and url/headers (HTTP)"
  - "Transport selection: command → stdio, url → HTTP"
  - "OAuth fields in server definition (tokenEndpoint, clientId, clientSecret, scopes)"
  - "list_mcp_resources tool always registered"
gaps:
  - "Managed-domains-only mode and enterprise policy enforcement not implemented"
  - "Orphaned plugin cache exclusion from search tools not implemented"
  - "OpenID Connect Discovery not supported in server definition"
defer_to: P4
depends_on:
  - cli
  - mcp-client
---
# MCP Configuration

## Overview

Jenny loads Model Context Protocol (MCP) server definitions from config files and CLI flags. Headless operators pass `--mcp-config` to attach servers without interactive setup.

## CLI

| Flag | Behavior |
|------|----------|
| `--mcp-config <path>…` | One or more JSON file paths; repeatable |
| `--strict-mcp-config` | Use **only** `--mcp-config` servers; ignore user/project/local/plugin sources |

Both stdio (`command` field) and HTTP (`url` field) servers are supported.

## Config Merge Precedence

When not in strict mode, configs merge in three phases (later phase wins on name collision):

**Phase 0 — Default configs** (loaded in order, later file wins):
1. `~/.agents/mcp.json` (cross-tool shared user config)
2. `<cwd>/.agents/mcp.json` (cross-tool shared project config)
3. `~/.jenny/mcp.json` (jenny-specific user config — highest default priority)

**Phase 1 — Plugin configs** (gap fill only, default configs win on collision):
4. Plugin bundled MCP server definitions

**Phase 2 — CLI override** (overrides all):
5. CLI `--mcp-config` files (explicit override, `maps.Copy` overwrites)

`~/.jenny/mcp.json` and `~/.agents/mcp.json` are loaded automatically (no CLI flag required) when not in bare or strict mode. This ensures MCP servers installed via the Portal UI (which writes `~/.jenny/mcp.json`) are also available in CLI sessions.

## Server Definition Shape

Each server entry supports:

```json
{
  "mcpServers": {
    "my-stdio-server": {
      "command": "npx",
      "args": ["-y", "@example/mcp-server"],
      "env": { "API_KEY": "${MY_API_KEY}" }
    },
    "my-http-server": {
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer ${TOKEN:-default}" },
      "tokenEndpoint": "https://auth.example.com/oauth/token",
      "clientId": "${MCP_CLIENT_ID}",
      "clientSecret": "${MCP_CLIENT_SECRET}",
      "scopes": ["read", "write"]
    }
  }
}
```

OAuth fields (`tokenEndpoint`, `clientId`, `clientSecret`, `scopes`) are optional. When `tokenEndpoint` is present, the client uses OAuth 2.1 refresh_token grant for 401 retry (see [`mcp-client.md`](./mcp-client.md)).

### Transport Selection

| Fields present | Transport used |
|---------------|----------------|
| `command` | Stdio (subprocess) |
| `url` (no `command`) | HTTP (Streamable HTTP) |
| Neither | Skipped (invalid) |

If both `command` and `url` are specified, `command` takes precedence (stdio transport).

### Environment Variable Expansion

Expand `${VAR}` and `${VAR:-default}` in:

- `command`, `args[]`, `env` values
- `url`, `headers` values

Unset `${VAR}` without default resolves to empty string. `${VAR:-}` (empty default) is also supported — behaves the same as an unset var.

## Planned Features (Not Implemented)

### Plugin Orphaned Cache Exclusion

**Status: not implemented (P4).** When plugins install MCP servers from zip caches, orphaned cache directories (plugin removed but cache remains) should be excluded from workspace search tools and MCP server discovery paths.

### Managed-Domains-Only Mode

**Status: not implemented (P4).** Enterprise policy may set `allowManagedDomainsOnly` for MCP network egress, restricting connections to policy-allowed servers only.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Invalid JSON in `--mcp-config` | Fail startup with parse error |
| Duplicate server names | Higher-precedence config wins |
| Missing env var for required secret | Resolves to empty string; fails at connect time if server rejects |
| Strict mode + empty `--mcp-config` | No MCP servers (not fallback to user config) |
| Relative paths in args | Passed to `exec.Command` as-is (no cwd resolution at config layer) |

## Headless Protocol Compatibility

- `system`/`init` stream-json line lists connected `mcp_servers` by name.
- Tool names exposed to model use prefix `mcp__<server>__<tool>` (see [`mcp-client.md`](./mcp-client.md)).

## Acceptance Criteria

- **AC1:** Multiple `--mcp-config` paths merge in order.
- **AC2:** Env expansion works for `${VAR}` and `${VAR:-default}`.
- **AC3:** Strict mode ignores non-CLI config sources.
- **AC4:** OAuth fields parsed from config and passed to HTTP transport.
- **AC5:** `list_mcp_resources` tool always registered (even with zero MCP servers).
