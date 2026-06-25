---
title: JSON Repair Pipeline
slug: json-repair
priority: P1
status: done
spec: complete
code: done
package: internal/jsonrepair, internal/api, internal/agent
depends_on:
  - sse-streaming
  - agent-loop
---
# JSON Repair Pipeline

## Overview

LLM-emitted tool call arguments and side-query JSON responses are often malformed: markdown fences, trailing commas, unquoted keys, single-quoted strings, Windows paths with unescaped backslashes, and truncated objects when output limits are hit. The repair pipeline normalizes this content at parse time only — it never modifies messages sent upstream, so prompt cache prefixes remain stable.

## Repair Layers

### Layer 1 — Pre-processing

Applied before structural repair:

- Strip markdown code fences (`` ```json `` … `` ``` `` and bare `` ``` `` wrappers).
- Pre-escape Windows drive-letter paths where backslash-letter sequences (e.g. `\t`, `\f`) would be misread as JSON escape codes.
- Remove trailing commas immediately before `}` or `]`.
- Trim leading and trailing whitespace.

Valid JSON that unmarshals successfully after trimming is returned unchanged without further mutation.

### Layer 2 — Structural repair

When standard unmarshaling still fails:

- Quote bare object keys (e.g. `{file_path: "x"}` → `{"file_path": "x"}`).
- Convert single-quoted strings to double-quoted strings.
- Close unmatched `{` / `[` by appending the missing brackets/braces.
- For truncated input (no closing `}`), attempt minimal structural closure so required fields can be recovered.

Each repair step is deterministic: identical input always yields identical output.

### Layer 3 — Tool-specific extraction

When structural repair fails and the tool name is `edit` or `write`:

- Extract `file_path`, `old_string`, and `new_string` (edit) or `file_path` and `content` (write) from semi-structured text using key-value pattern matching.
- Handles source code embedded in values that contains unescaped quotes breaking JSON structure.

### Layer 4 — Diagnostic errors

When all repair attempts fail, the error describes:

- Which argument keys were found versus which are required for the tool.
- An example of a correct call shape.

Raw low-level unmarshaling position errors are not surfaced to callers.

## JSON Extraction

Text responses that embed a JSON object (complexity eval, progress eval, QA verdict) are scanned with brace-depth tracking that respects string literals and escape sequences — not naive first-`{` to last-`}` slicing.

## Integration

### Streaming tool input finalization

Accumulated partial JSON for each streaming `tool_use` block passes through the repair pipeline instead of bare unmarshaling. On success the repaired map is stored as tool input. On failure a diagnostic parse error is attached to the block for downstream reporting.

### Side-query and eval parsing

Complexity evaluation, progress evaluation, and QA verdict parsing use JSON extraction plus repair before unmarshaling into typed structs.

### Truncated-response retry

When tool input finalization fails (empty or unparseable arguments) **and** the response was truncated (`stop_reason` is `max_tokens` or the stream did not receive `message_stop`):

- Inject a `<system-reminder>` instructing the model that its last response was truncated and to retry with shorter output.
- Do not persist the incomplete assistant turn to the transcript.
- Do not execute tools from that turn.
- The retry does not consume an agent-loop iteration (same treatment as output-cap truncation continue).

## Constraints

- Pure Go; no third-party JSON repair libraries.
- Parse-time only — no effect on outbound API payloads or cache keys.
- Deterministic for all inputs.

## Acceptance Criteria

- **AC1:** Markdown fence stripping recovers valid objects from fenced LLM output.
- **AC2:** Windows paths such as `D:\test\file.py` and `C:\Users\name\Documents` parse without escape-sequence corruption.
- **AC3:** Trailing commas, unquoted keys, and single-quoted strings are repaired into valid JSON.
- **AC4:** Truncated JSON (missing closing brace) is closed minimally when possible.
- **AC5:** Edit and write tool calls with broken quoting in `old_string` / `new_string` / `content` are recovered via tool-specific extraction.
- **AC6:** Already-valid JSON is returned without semantic change.
- **AC7:** Brace-depth JSON extraction ignores braces inside string literals.
- **AC8:** Truncated streaming tool input with failed parse triggers a non-persisted retry with a system reminder instead of executing tools with empty arguments.
- **AC9:** Repair failure returns structured diagnostics (found keys, missing keys, example format).
