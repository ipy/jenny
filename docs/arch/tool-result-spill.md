---
title: Tool Result Spill
slug: tool-result-spill
priority: P1
status: done
spec: complete
code: done
package: internal/tool
depends_on:
  - tool-registry
---

# Tool Result Spill

## Overview

When a tool produces output that exceeds the inline size limit (~30 KB), the full content is written to a session-scoped spill file on disk. The tool result returned to the model contains a structured preview plus the spill path, keeping the message chain within token budget without losing access to the full output.

Spilling happens at tool execution time, before results enter the message chain. It does not affect prompt cache keys.

## Inline Limit

The default inline cap is **30,720 characters** (~30 KB). Content at or below this limit is returned unchanged.

## Spill Directory Resolution

Spill files are created with unique names matching the pattern `spill-*`. Resolution order:

1. **Session home**: `$JENNY_HOME/sessions/<session-id>/spills/` (preferred when a session ID is set)
2. **Project local**: `<project-root>/.jenny/sessions/<session-id>/spills/` (when project root and session ID are available)
3. **System fallback**: `$TMPDIR/jenny-spills/<session-id>/` (or `$TMPDIR/jenny-spills/` when no session ID)

Each spill creates a separate file. Multiple large results in the same session do not overwrite prior spills.

## Preview Format

When content is spilled, the inline tool result uses this structure:

```
[Output too large for inline display (N chars). Full output saved to: /path/to/spill-file]

First 1024 chars:
<first 1024 characters>

Last 512 chars:
<last 512 characters>

Use the Read tool to view the full output if needed.
```

If writing the spill file fails, the tool falls back to inline truncation with an error note rather than returning the full oversized content.

## Tool Result Metadata

The tool result `truncated` flag is set to `true` when output was spilled or otherwise truncated for size. Spilled results are not errors.

## Applicable Tools

Spill checks apply to tools that can return large text payloads:

| Tool | When spill runs |
|------|-----------------|
| Bash | After command stdout/stderr is combined |
| Grep | After native line limits and character cap |
| Read | Full-file reads (no offset/limit) after formatted output is built |
| call_mcp | After MCP tool response is received |
| lsp | After LSP response text is assembled |
| web_fetch | After HTML conversion and native content cap |
| web_search | After search results are rendered |

Tools that previously had per-tool content caps (Grep 20K char limit, WebFetch 128K char cap) no longer apply those limits. Spill is the **single size boundary** for tool result content. Semantic limits (Grep `head_limit` for match count, WebFetch `webFetchMaxReadBytes` for network read) are preserved since they serve different purposes (relevance filtering and memory safety respectively).

## Session Awareness

Tools receive the active session ID and project root (working directory) so spill paths are scoped correctly. See [jenny-directory-structure](./jenny-directory-structure.md) for spill directory layout under `sessions/<id>/spills/`.

## Compaction

Context compaction does not perform a separate tool-result truncation pass. Execution-time spilling is the single mechanism for bounding oversized tool output in the message chain.
