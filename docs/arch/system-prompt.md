---
title: System Prompt Assembly
slug: system-prompt
priority: P1
status: done
spec: complete
code: done
package: internal/agent
depends_on:
  - git-helpers (done)
  - tool-registry
---
# System Prompt Assembly

## Overview

Jenny assembles the model system prompt from static sections, dynamic context (cwd, git, tools), and optional user overrides. The assembled prompt is returned as a **slice of logical blocks** (`[]string`) ordered by stability to maximize cross-session and cross-project prompt caching.

## Prompt Caching Design

Anthropic prompt caching (`prompt-caching-2024-07-31`) relies on byte-for-byte stability of the request prefix. Jenny uses a multi-block system prompt to isolate volatile segments from stable ones.

### Stability-Ordered Multi-Block Assembly

The system prompt is split into 4 blocks, from most stable to most volatile:

| Block | Stability | Content | Implementation |
|-------|-----------|---------|----------------|
| 1 | Global (Most stable) | Default intro + tool list + redaction instructions | `buildSystemPrompt()` |
| 2 | Machine/Session stable | Platform (OS/Arch) + skills manifest | `buildSystemPrompt()` |
| 3 | Project stable | **CWD (Working Directory)** + Memory content (`AGENTS.md`) | `buildSystemPrompt()` |
| 4 | Turn stable (Most volatile) | Current Date + Git status | `buildSystemPrompt()` |
| 5 | User customization | `prependSystemPrompt` + `appendSystemPrompt` | Prepended before Block 1 and/or appended after Block 4 |

Rationale: Placing CWD in Block 3 ensures that if you switch projects, Block 1 and 2 (OS/Arch and Jenny version) can still hit the cache. Placing Date and Git Status in Block 4 ensures that even if they change across sessions, the first 3 blocks remain cache-hit candidates.

### System Prompt Freeze (Process-Level)

The assembled system blocks are **frozen on first call** and cached in StreamConfig. Subsequent calls to `AssembleSystemPrompt` within the same process return the identical slice — regardless of git status changes, date changes, or memory content updates across turns.

### Cache-Control Marker

In the Anthropic provider, system prompt blocks are sent as an array of content blocks. The `cache_control: { type: "ephemeral" }` marker is placed on the final block of the main system prompt (Block 4 or Block 5 if custom append is used), maximizing cache hits for the entire prefix.

### Resume Persistence (Cross-Process)

On first assembly, the frozen system prompt blocks are joined with `\n\n` and persisted to the transcript as a `state` entry with field `system_prompt`. On session resume with `-r`, the cached system prompt is restored from the transcript — ensuring the same system prompt bytes are sent across process boundaries.

## Assembly Flow

```
AssembleSystemPrompt(cfg, tools, cwd)
    │
    ├─ cfg.CachedSystemPrompt set?
    │     YES → return it (frozen []string, cache-friendly)
    │     NO  → buildSystemPrompt() → returns 4 blocks
    │           → freeze into cfg.CachedSystemPrompt
    │           → persist joined string to transcript
    │
    └─ API call: multiple system blocks with cache_control on last stable block
```

## Default Sections

Static blocks: intro, system identity, doing tasks, actions, tone, using tools.

Dynamic registry sections (when enabled): memory, environment, MCP status, scratchpad, skills manifest.

## Scratchpad Directory

The scratchpad is a session-scoped temporary directory (`~/.jenny/sessions/<sessionID>/scratchpad`).

The default intro instructs the agent to use it for all intermediate files. To make the path accessible without exposing the real filesystem path:

- **Shell tools** (`Bash`): the `JENNY_SCRATCHPAD` environment variable is injected at execution time, pointing to the real scratchpad path.
- **Read/Write/Edit tools**: paths prefixed with `$JENNY_SCRATCHPAD/` are resolved to the real scratchpad path before permission checks.

The prefix substitution happens **before** the relative-path join with `cwd`, so the agent can write `$JENNY_SCRATCHPAD/foo.txt` regardless of the current working directory. The resolved path is cleaned and validated to be under the scratchpad directory, preventing escape via `..` or symlinks.

The scratchpad prefix is resolved before the relative-path join with cwd.

## DynamicSystemSuffix

`DynamicSystemSuffix` is **intentionally empty** in the current architecture. All dynamic updates that occur *after* the session has started (e.g., directory changes via `cd`, new skills discovered) are communicated via **System Reminders** (virtual user messages) at the end of the message chain. 

This prevents any change in the environment from busting the system prompt cache prefix for the entire conversation history.

## Injected Context

| Section | Source | Limits | Block |
|---------|--------|--------|-------|
| User context | Project instruction files | Truncated per policy | 3 |
| System identity | Default identity keywords | — | 1 |
| Git status | branch, status snapshot | Max 2000 chars | 4 |
| Cwd | Current working directory | Absolute path | 3 |
| Platform | OS, arch | — | 2 |
| Date | time.Now() (YYYY-MM-DD) | — | 4 |

Git status truncated at 2000 characters with ellipsis. Date and Git Status are in the most volatile Block 4. CWD and Memory are in Block 3. Platform and Skills are in Block 2. Global identity and tools are in Block 1.

## Tool List Sync

`getUsingYourToolsSection(enabledTools)` built from **actually registered** tool names at runtime.

Must mention available tools (Read, Edit, Write, Glob, Grep, Bash) or embedded-search variants when Glob/Grep omitted.

When tool search enabled: omit deferred tool **descriptions** from API schemas (`deferLoading: true`); optional meta message lists deferred tool names.

## Override Rules

| Input | Effect |
|-------|--------|
| `customSystemPrompt` | Replaces all default sections |
| `prependSystemPrompt` | Prepended before Block 1 (unless `overrideSystemPrompt` set) |
| `appendSystemPrompt` | Appended as Block 5 after Date/Git (unless `overrideSystemPrompt` set) |
| `overrideSystemPrompt` | Suppresses both prepend and append |

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Empty custom prompt | Valid but minimal; still inject prepend/append if set |
| Both prepend and append set | Prepend appears first (before Block 1), append appears last (after Block 4) |
| Worktree cwd switch | Not supported across resume; new process re-freezes |
| MCP servers connect mid-session | Refresh MCP section next turn |
| `--bare` mode | Skip skills, memory, non-essential sections |
| Resume with different git status | Uses frozen prompt from transcript; suffix reflects new status |
| Cross-process same session ID | System prompt restored from transcript; cache may or may not hit depending on TTL |

## Acceptance Criteria

- **AC1:** Custom system prompt replaces defaults entirely.
- **AC2:** Tool list matches registered tools exactly.
- **AC3:** Git status injected with 2000 char cap.
- **AC4:** Deferred tools omitted from schemas when tool search on.
- **AC5:** appendSystemPrompt appended unless override suppresses.
- **AC5b:** prependSystemPrompt prepended before Block 1 unless override suppresses.
- **AC6:** Default intro includes "autonomous" and "non-interactive" identity keywords
- **AC7:** Assembled prompt >= 1000 chars with default tools and no git repo
- **AC8:** Default intro includes bash-safety language ("destructive" or "rm -rf")
- **AC9:** Default prompt names Glob and Grep as search tools
- **AC10:** No unfilled template placeholders in assembled output
- **AC11:** AC1–AC5 from iter-115 continue to pass (regression)
- **AC12:** `--print-system-prompt` stdout contains "date" or "Date" (date is injected into Block 4).
- **AC13:** The injected date reflects the current calendar year.
- **AC14:** `--print-system-prompt` stdout contains OS/platform info (`"Platform"`, `"darwin"`, `"linux"`, or `"windows"`).
- **AC15:** When jenny is launched from inside a git repository, `--print-system-prompt` stdout contains the current branch (substring `"Branch"` or `"Git context"`).
- **AC16:** When jenny is launched from a directory with no git repo, `--print-system-prompt` stdout does NOT contain `"Git context"` or `"Branch:"`.
- **AC17:** CLAUDE.md content from cwd appears in system prompt as a `<system-reminder>` block (Block 3).
- **AC18:** AGENTS.md used as fallback when CLAUDE.md absent.
- **AC19:** CLAUDE.md takes precedence when both files exist.
- **AC20:** Subdirectory CLAUDE.md is not loaded.
- **AC21:** Two calls to `AssembleSystemPrompt` with same cfg in same process return identical slices.
- **AC22:** `DynamicSystemSuffix` is always empty to protect cache prefix stability.
- **AC23:** Anthropic API request body.System is an array of content blocks with `cache_control` on the last stable block.
- **AC24:** Resume restores `CachedSystemPrompt` from transcript; system prompt does not change.
- **AC25:** `--print-system-prompt` output ends with a newline character to prevent shell prompt overlap.
- **AC26:** Default intro mentions the scratchpad directory and the `$JENNY_SCRATCHPAD` prefix for Read/Write/Edit tools.
- **AC27:** OpenAI Responses API uses `instructions` field for the first (most stable) block.
- **AC28:** CWD is located in Block 3, making it stable within a project but distinct across projects.
