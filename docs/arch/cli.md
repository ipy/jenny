---
title: CLI
slug: cli
priority: P0
status: done
spec: complete
code: done
package: internal/cli
gaps: []
defer_to: P3
depends_on:
  []
---
# CLI

## Overview

Jenny CLI is headless-only: accept a prompt, run the agent loop, emit text or stream-json, exit with status code.

## Usage

```bash
jenny [flags] [prompt]
jenny -p "prompt text"
```

## Flags

| Flag | Description |
|------|-------------|
| `-p`, `--print <prompt>` | Prompt string (non-interactive) |
| `--version`, `-v` | Prints `<semver> (jenny)` and exits 0. |
| `--model <name>` | Override model (beats `ANTHROPIC_MODEL` env) |
| `-r`, `--resume <session_id>` | Resume session from transcript |
| `--continue` | Resume most recent session in project |
| `--fork-session` | Fork resumed session to new ID |
| `--output-format <fmt>` | `text` (default) or `stream-json` |
| `--include-partial-messages` | Include partial messages in output (requires `--output-format stream-json`) |
| `--mcp-config <path>…` | MCP configuration file path(s) (can be specified multiple times) |
| `--strict-mcp-config` | Only use `--mcp-config` servers |
| `--no-session-persistence` | Disable session persistence |
| `--verbose` | Debug logging to stderr |
| `--dangerously-skip-permissions` | Bypass permission/classifier gates (maps to `--permission-level unrestricted`) |
| `--permission-level <level>` | Set permission level: `read`, `analyze`, `edit` (default), `execute`, `unrestricted`. See [permission-levels.md](../patterns/permission-levels.md). |
| `--system-prompt <text>` | Replace the default system prompt entirely |
| `--append-system-prompt <text>` | Append text after the assembled default system prompt |
| `--prepend-system-prompt <text>` | Prepend text before the assembled default system prompt |
| `--print-system-prompt` | Print the assembled system prompt and exit (no API call) |
| `--max-iterations <n>` | Maximum raw loop iterations (0 = unlimited) |
| `--max-turns <n>` | Maximum number of turns (0 = unlimited) |
| `--max-budget-usd <n>` | Budget limit in USD (0.0 = no limit) |
| `--redact <mode>` | Secret redaction mode: `disabled`, `redact` (default), `recover`. See [secret-redaction.md](./secret-redaction.md). Overrides `JENNY_REDACT`. |
| `--transcript-dir <path>` | Override transcript directory. Overrides `JENNY_TRANSCRIPT_DIR`. |
| `--max-tool-concurrency <n>` | Max parallel tool executions. Overrides `JENNY_MAX_TOOL_CONCURRENCY`. |
| `--compact-keep-archive` | Keep `<id>.tar.gz` after resume extraction. Overrides `JENNY_COMPACT_KEEP_ARCHIVE`. |
| `--disable-compact` | Disable all compaction. Overrides `JENNY_DISABLE_COMPACT`. |
| `--disable-auto-compact` | Disable auto-compact only. Overrides `JENNY_DISABLE_AUTO_COMPACT`. |
| `--enable-session-memory` | Enable session-memory compaction branch. Overrides `JENNY_ENABLE_SESSION_MEMORY`. |
| `--disable-auto-memory` | Disable auto-memory directory. Overrides `JENNY_DISABLE_AUTO_MEMORY`. |
| `--bare` | Disable skill discovery for minimal environments. |
| `--swarm` | Enable swarm mode for named agent delegation. |
| `--deny-tool <name>…` | Tool name to deny (can be specified multiple times). |
| `--effort <level>` | Reasoning effort level (`low`, `medium`, `high`) for OpenAI o-series and DeepSeek models. |
| `--thinking-budget <n>` | Maximum thinking tokens for Anthropic extended thinking. |
| `--refresh-registry` | Synchronously fetch the latest model registry. |
| `--offline` | Skip all network fetch, use cached data as-is. |

## Flag Rules

| Rule | Behavior |
|------|----------|
| No prompt | Print usage; exit non-zero |
| Multiple `-p` flags | Joins values with a single newline |
| Positional + `-p` both given | When `-p` is set, positional args are **ignored**; only `-p` values are used |
| `--output-format stream-json` | Requires prompt (`-p` or positional) |
| `--include-partial-messages` | Requires `--output-format stream-json` |
| `--continue` with no prior sessions | Exit non-zero with error "no sessions to continue" |
| Both `--dangerously-skip-permissions` and `--permission-level` | `--dangerously-skip-permissions` wins (unrestricted); warning logged |
| `--permission-level` without value | Exit non-zero with pflag error "flag needs an argument: --permission-level" |
| Invalid `--permission-level` value | Exit non-zero with error listing valid levels |
| `--fork-session` without `-r` | Exit non-zero with error "--fork-session requires -r/--resume" |
| `--continue` with `-r` | Exit non-zero with error "--continue is mutually exclusive with -r/--resume" |
| `--continue` with `--no-session-persistence` | Exit non-zero with error "--continue requires session persistence" |
| `--refresh-registry` with `--offline` | Exit non-zero with error "--refresh-registry and --offline are mutually exclusive" |
| Boolean flag negation | `--flag=false` is accepted and overrides a `true` set in `.jenny/config.json` or via `JENNY_*`. The absence of the flag is the only way to leave the value at its low-precedence default. `--no-<flag>` is **not** a registered form. |

## Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success |
| Non-zero | Missing prompt, API error, agent error, session not found |
| Non-zero | Unknown or invalid flag |

On exit (normal, Ctrl+C, budget exceeded, or max turns), the process performs an ordered shutdown: flush transcript data to disk, drain memory extraction goroutines with bounded timeout, and disconnect MCP clients with bounded timeout.

Help (`-h`) exits 0. Version (`--version`) uses `constants.Version` for unified reporting.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `.env` Loading | Auto-loads `./.env` and `./.jenny/.env` if present |
| `ANTHROPIC_BASE_URL` | API endpoint |
| `ANTHROPIC_AUTH_TOKEN` | Auth token — forwarded as `Authorization: Bearer <token>` |
| `ANTHROPIC_API_KEY` | API key sent as `X-Api-Key` header. When set, takes precedence over `ANTHROPIC_AUTH_TOKEN`. |
| `ANTHROPIC_BETAS` | Comma-separated list of additional `anthropic-beta` header values. |
| `ANTHROPIC_MODEL` | Default model — overridden by `--model` flag when both are set |
| `API_TIMEOUT_MS` | Timeout for API requests in milliseconds (default: 3600000, or 60 minutes). |
| `DEBUG` | Enable debug logging. Values: `1`, `true`, `yes`, `on`. Alias for `JENNY_DEBUG`. |
| `HTTP_PROXY` | HTTP proxy URL for API requests. |
| `HTTPS_PROXY` | HTTPS proxy URL for API requests. |
| `JENNY_DEBUG` | Enable debug slog (`1` = DEBUG) |
| `JENNY_PERMISSION_LEVEL` | Default permission level when `--permission-level` not specified. Values: `read`, `analyze`, `edit`, `execute`, `unrestricted`. Flag overrides env. |
| `JENNY_REDACT` | Default secret redaction mode (`disabled`, `redact` (default), `recover`). `--redact` overrides this. |
| `JENNY_TRANSCRIPT_DIR` | Override transcript directory (default: `~/.jenny/transcripts`). `--transcript-dir` overrides this. |
| `JENNY_MAX_TOOL_CONCURRENCY` | Default max parallel tool executions. `--max-tool-concurrency` overrides this. |
| `JENNY_COMPACT_KEEP_ARCHIVE` | Keep `<id>.tar.gz` after resume extraction. `--compact-keep-archive` overrides this. |
| `JENNY_DISABLE_COMPACT` | Disable all compaction. `--disable-compact` overrides this. |
| `JENNY_DISABLE_AUTO_COMPACT` | Disable auto-compact only. `--disable-auto-compact` overrides this. |
| `JENNY_ENABLE_SESSION_MEMORY` | Enable session-memory compaction branch. `--enable-session-memory` overrides this. |
| `JENNY_DISABLE_AUTO_MEMORY` | Disable auto-memory directory. `--disable-auto-memory` overrides this. |
| `JENNY_BARE` | Disable skill discovery. `--bare` overrides this. |
| `JENNY_SWARM` | Enable swarm mode. `--swarm` overrides this. |
| `JENNY_DENY_TOOL` | Denied tool names. `--deny-tool` overrides this. |
| `JENNY_EFFORT` | Reasoning effort level. `--effort` overrides this. |
| `JENNY_THINKING_BUDGET` | Max thinking tokens. `--thinking-budget` overrides this. |
| `JENNY_ROUTES_PROFILE` | Select active routing profile. Config/env only, no CLI flag. |
| `JENNY_REFRESH_REGISTRY` | Synchronously fetch model registry. `--refresh-registry` overrides this. |
| `JENNY_OFFLINE` | Skip network fetch. `--offline` overrides this. |
| `NO_PROXY` | Comma-separated list of domains to bypass proxy for. |
| `OPENAI_BASE_URL` | Base URL for OpenAI-compatible API (e.g., `https://api.openai.com/v1`). When set, selects the OpenAI provider instead of the default Anthropic provider. |
| `OPENAI_API_KEY` | API key for OpenAI-compatible backend. Sent as `Authorization: Bearer <key>`. |
| `OPENAI_DEFAULT_MODEL` | Default model name for OpenAI provider (e.g., `gpt-5.4-nano`). Takes precedence over `ANTHROPIC_MODEL` when OpenAI provider is active. |
| `OPENAI_WIRE_API` | Wire protocol version for OpenAI API. Supported values: `chat` (default) or `responses` (Responses API). |

## Configuration Precedence

All Jenny-owned env vars and CLI flags listed above go through the unified [koanf-config.md](./koanf-config.md) layer. The full precedence is:

1. CLI flag (highest)
2. `JENNY_*` env var
3. `.jenny/config.json` field
4. Built-in default (lowest)

## Jenny Gaps vs Target Spec

All flags listed in the Flags table above are wired in code. No known feature gaps remain.

## Acceptance Criteria

- **AC1:** No prompt → usage + non-zero exit.
- **AC2:** `--model` overrides env model.
- **AC3:** `-r` loads transcript and preserves session ID in output.
- **AC4:** stream-json writes JSON lines to stdout only.
- **AC5:** `--verbose` / `JENNY_DEBUG` logs to stderr without polluting stdout.

## Related

- Stream protocol: [`stream-json.md`](./stream-json.md)
- Session resume: [`session-resume.md`](./session-resume.md)
