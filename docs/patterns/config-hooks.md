---
title: Config File Hooks
slug: config-hooks
priority: P3
status: partial
spec: complete
code: partial
package: internal/agent
gaps: []
depends_on:
  - koanf-config
---
# Config File Hooks

## Overview

Hooks allow running shell commands at specific points during agent execution. Hooks are loaded from `settings.json` (global or project-level) and executed synchronously before/after tool use.

## Configuration

Hooks are stored in `settings.json` under the `hooks` key:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "./check.sh", "timeout": 60 }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write",
        "hooks": [
          { "type": "command", "command": "echo 'file written'" }
        ]
      }
    ],
    "Notification": [
      {
        "hooks": [
          { "type": "command", "command": "osascript -e 'display notification \"Jenny done\"'" }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "echo 'session ended'" }
        ]
      }
    ]
  }
}
```

## Hook Events

| Event | When | Matcher | Can block |
|-------|------|---------|-----------|
| `PreToolUse` | Before a tool executes | Tool name | Yes (non-zero exit blocks tool) |
| `PostToolUse` | After a tool executes | Tool name | No |
| `Notification` | Agent wants to notify user | None | No |
| `Stop` | Agent session ends | None | No |

## Hook Entry Structure

Each hook entry has:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `matcher` | string | No | Tool name to match (only for PreToolUse/PostToolUse). Empty means match all. |
| `hooks` | array | Yes | List of hook commands |

Each hook command has:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Must be `"command"` |
| `command` | string | Yes | Shell command to execute (via `sh -c`) |
| `timeout` | int | No | Timeout in seconds (default: 60) |

## PreToolUse Behavior

PreToolUse hooks receive tool context as environment variables:

| Variable | Value |
|----------|-------|
| `TOOL_NAME` | Name of the tool being invoked |
| `TOOL_INPUT` | JSON-encoded tool input parameters |

If the hook command exits with non-zero status, the tool invocation is **blocked**.

## PostToolUse Behavior

PostToolUse hooks receive tool context as environment variables:

| Variable | Value |
|----------|-------|
| `TOOL_NAME` | Name of the tool that was invoked |
| `TOOL_INPUT` | JSON-encoded tool input parameters |
| `TOOL_OUTPUT` | Tool output (truncated to 10KB) |

## Loading

Hooks are loaded from the koanf config layer alongside other config. Project hooks override global hooks for the same event type.
