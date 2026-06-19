---
title: Jenny Directory Structure
slug: jenny-directory-structure
priority: P2
status: done
spec: complete
code: done
package: internal/constants, internal/session, internal/tool
depends_on:
  - session-persistence
  - cost-tracking
---

# .jenny Directory Structure

This document describes the organization of the `.jenny` directory, which is the local storage for the Jenny agent. Jenny uses a session-specific structure where all conversation data is encapsulated within unique session directories.

## Directory Location
The `.jenny` directory can exist in two locations:
1.  **Global Home**: `~/.jenny/` (User-wide defaults and global sessions).
2.  **Project Local**: `<project-root>/.jenny/` (Project-specific session data).

---

## Global Directory Layout (`~/.jenny/`)

| Path | Description |
| :--- | :--- |
| `sessions/` | Root for session-specific data isolation. |
| `sessions/<id>/` | Dedicated directory for a specific session. |
| `transcripts/` | Legacy directory — created at startup by `NewManager` for backward compatibility. New sessions use `sessions/<id>/transcript.jsonl` instead. |
| `config` | Global cost state fallback (used when no session ID is set). |

### Project-Local Layout (`<project-root>/.jenny/`)

The project-local `.jenny/` directory may contain:
- `config.json` — project-specific configuration (model overrides, permission level, etc.)
- `pricing.json` — custom per-model pricing overrides
- `.env` — environment variable overrides
- `mcp-resources/` — MCP resource persistence scoped to project

---

## Session Directory Layout (`sessions/<id>/`)

Each session has a dedicated folder to prevent file "sprawl" and ensure all data related to a single conversation is co-located.

### 1. Core Session Data
*   **`transcript.jsonl`**: The source of truth for the conversation. Contains all messages, tool calls, tool results, and state snapshots.
*   **`config`**: Session-specific cost state, token usage, and metadata.
*   **`memory.md`**: The "long-term memory" for this session. A summarized markdown file that the agent uses to maintain context.

### 2. Tool-Specific Subdirectories
*   **`spills/`**: Stores large command outputs from `BashTool`.
    *   Files follow the pattern `spill-*`.
    *   Used when tool output exceeds the inline protocol limit (30KB).
*   **`scratchpad/`**: A "safe" temporary workspace for the agent.
    *   Tools like `Read`, `Write`, and `Edit` can access this directory even if they are restricted from accessing the broader filesystem.
*   **`mcp-resources/`**: Stores binary blobs or large resources fetched via MCP (Model Context Protocol).
    *   Files are typically saved as `.bin` or structured resource exports.

---

## Enforcement

Jenny strictly enforces this nested structure for all data persistence:

1.  **Path Resolution**: Session-related paths use `constants.SessionDir(id)`, with `ScratchpadDir()` and `SpillsDir()` as dedicated helpers for their respective subdirectories.
2.  **Isolation**: Tools are wired with the session ID to ensure their outputs are correctly scoped.
3.  **Cost State**: Cost tracking is persisted per-session within the session's `config` file.
