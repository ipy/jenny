---
title: Core Agent Loop
slug: agent-loop
priority: P0
status: done
spec: complete
code: done
package: internal/agent, internal/tool
defer_to: P3
depends_on:
  - anthropic-api-client
  - query-engine
  - stream-json
  - session-persistence
  - parallel-tool-execution
  - message-normalization
  - tool-registry
  - cost-tracking
  - context-compaction
  - secret-redaction
  - session-memory
  - system-prompt
gaps:
  - tool coverage partial — see tool-registry.md
  - engine lifecycle details → query-engine.md
---
# Core Agent Loop

## Overview

The core agent loop implements a minimal viable pipeline for AI-driven tool execution:
- Prompt in → API call with tools → Tool execution → Loop back → Text out

## Architecture

```
User Input → RunStream()/QueryEngine.SubmitMessage()
                              ↓
                     API Client → Provider API (Anthropic/OpenAI/etc.)
                              ↓
                     stop_reason == "tool_use"?
                    / \
                  Yes                        No
                   | |
            Execute tools          Return text output
            Send results
                   |                         |
            Loop back to API         Final response
```

## Components

### Agent Loop

The main agent loop orchestrates the interaction:

1. **Initialization**: Sets up API client, working directory, and initial messages
2. **Tool Conversion**: Converts tool definitions to API format
3. **Main Loop**: Iterates sending messages and processing responses (configurable via `--max-iterations`; default unlimited)
4. **Response Processing**: Handles text and tool_use content blocks
5. **Tool Execution**: Executes requested tools and collects results
6. **Message Building**: Constructs proper message payloads with tool_use blocks and tool_results

### Tool Implementations

Tools are assembled via `tool.Registry` (see [tool-registry.md](./tool-registry.md) for the full list). The registry wires 25+ tools across categories:

- **Filesystem**: Read, Write, Edit, NotebookEdit, Glob, Grep
- **Shell**: Bash (with read-only mode and dangerous command gating)
- **Web**: WebSearch, WebFetch
- **Tasks**: TaskCreate, TaskUpdate, TaskList, TaskStop, TaskOutput
- **MCP**: ReadMcpResource, McpPrompt, ListMcpResources
- **Subagent**: Subagent (worktree-based)
- **LSP**: LSP tool for code intelligence
- **Skills**: ActivateSkill

All tools implement the `tool.Tool` interface from `internal/tool/`.

### API Client

API client wrapper:
- Converts internal message format to SDK format
- Handles tool_use blocks and tool_results
- Returns structured response with content blocks

## Tool Execution Flow

1. Model returns `tool_use` block with tool name and input
2. Agent finds the corresponding tool via `FindTool()`
3. Tool executes with provided input and cwd
4. Result is appended to messages as `tool_result` content block
5. Loop continues until `stop_reason == "end_turn"`

### Termination

Empty or unrecognized `stop_reason` values are treated as `end_turn` (terminal). The loop **NEVER** continues on unrecognized `stop_reason` values. This prevents an infinite-loop bug where a text-only response with an empty `stop_reason` would cause the engine to re-query with the same duplicated assistant message.

If a response carries a `tool_use` block but `stop_reason` is empty (defensive path, should not occur per API contract but may occur with proxies), the loop treats this as `tool_use` and continues to execute the tool to keep the chain valid.

#### Context Cancellation

The loop respects the provided `context.Context`. If the context is cancelled (e.g., via SIGINT or timeout), the loop terminates and returns the context error. This ensures graceful shutdown during long-running operations.

## Security

### Path Traversal Prevention

The ReadTool enforces that file access stays within the working directory:

```go
// Get the absolute paths
absCwd, _ := filepath.Abs(cwd)
absFilePath, _ := filepath.Abs(filePath)

// Check that file's directory is within cwd
fileDir := filepath.Dir(absFilePath)
if fileDir != absCwd {
    // Reject traversal attempts
}
```

This ensures that even with symlinks or path manipulation attempts, files outside the working directory are rejected.

### Read-Only Bash Commands

BashTool enforces a read-only allowlist by default:

```go
func isReadOnlyCommand(command string) bool {
    readOnlyCommands := []string{
        "ls", "pwd", "whoami", "cat", "head", "tail", "grep", "find", "wc",
        "echo", "date", "which", "type", "file", "stat", "diff", "sleep",
    }
    // Check command against allowlist
}
```

Only commands starting with these prefixes are allowed in read-only mode.

## Message Format

### Assistant Message with Tool Use

When the model requests a tool call, the assistant message must include the `tool_use` block:

```go
messages = append(messages, api.Message{
    Role:    "assistant",
    Content: "",
    ToolUse: []ToolUseBlock{{
        ID:   block.ToolID,
        Name: block.ToolName,
        Input: block.ToolInput,
    }},
})
```

### Tool Result Message

Tool results are sent as user messages with tool_result content:

```go
messages = append(messages, api.Message{
    Role:    "user",
    Content: "",
    ToolResults: []ToolResultBlock{{
        ToolUseID: tr.ToolUseID,
        Content: tr.Content,
    }},
})
```

## Usage

The primary API is `RunStream` with `StreamConfig` (supports streaming, cost tracking, session persistence, compaction, and all P1+ features). `agent.Run()` is a legacy simple-path function without these capabilities.

```go
cfg := &agent.StreamConfig{
    Enabled:       true,
    MaxIterations: 200,
    SessionID:     sessionID,
    // ... other fields
}
engine, result, sessionID, err := agent.RunStream(ctx, prompt, tools, cwd, cfg, model)
```

For multi-turn interactions, use `QueryEngine.SubmitMessage()` directly (see [query-engine.md](./query-engine.md)).

## CLI

```bash
# Basic usage with positional argument
jenny "list the files in the current directory"

# Using -p flag
jenny -p "list the files in the current directory"

# Using --model flag to specify a model
jenny --model deepseek-v4-flash -p "say hello"

# Streaming JSON output (NDJSON format)
jenny --output-format stream-json -p "what is 2+2?"
```

### CLI Flags

| Flag | Description |
|------|-------------|
| `-p <prompt>` | Prompt to send to the agent |
| `--model <model>` | Model to use (overrides ANTHROPIC_MODEL env var) |
| `--output-format <format>` | Output format: `text` (default), `stream-json` |
| `--max-iterations <n>` | Maximum loop iterations (0 = unlimited) |
| `--verbose` | Enable verbose/debug output to stderr |
| `--include-partial-messages` | Include partial messages in stream-json output |
| `--dangerously-skip-permissions` | Bypass permission gates (maps to `--permission-level unrestricted`) |
| `--permission-level <level>` | Set permission level: `read`, `analyze`, `edit`, `execute`, `unrestricted` |
| `-r <session_id>` | Resume an existing session |
| `--mcp-config <path>` | MCP configuration file path (can be specified multiple times) |

### Exit Codes

- 0: Success
- Non-zero: Error (with stderr message)

### Streaming JSON Output (stream-json)

When using `--output-format stream-json`, each output line is a JSON object. The protocol uses these top-level types: `assistant` (with nested content blocks including thinking/text/tool_use), `user` (with tool_result content), `tool_progress`, `system` (for init and thinking_tokens), `stream_event` (raw SDK passthrough), and `result` (final summary).

See [stream-json.md](./stream-json.md) and [stream-json-spec.md](./stream-json-spec.md) for the full protocol specification and examples.

## Configuration

The agent reads configuration from the unified koanf layer — see [koanf-config.md](./koanf-config.md) for the full env-var / CLI-flag / JSON precedence rule. Jenny-owned env vars relevant to the agent loop:

| Variable | Description | Default |
|----------|-------------|---------|
| `JENNY_MAX_TOOL_CONCURRENCY` | Max parallel tool executions | `10` |
| `DEBUG` | Enable debug-level structured logging to stderr | (none) |
| `JENNY_DEBUG` | Enable debug-level structured logging to stderr | (none) |
| `JENNY_VERBOSE` | Enable debug-level structured logging to stderr | (none) |

Example `.env` file:
```
ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic
ANTHROPIC_AUTH_TOKEN=your-token-here
ANTHROPIC_MODEL=deepseek-v4-flash
```

## Logging

The agent uses Go's `log/slog` for structured logging. All log output is written to stderr to keep stdout clean for agent responses.

| `DEBUG` / `JENNY_DEBUG` / `JENNY_VERBOSE` | Log level | Example output |
|------------------------------------------|-----------|----------------|
| all unset | `INFO` and above only | `level=INFO msg="Sending message" model=deepseek-v4-flash` |
| any set to `1` | `DEBUG` and above | `level=DEBUG msg="Sending message" model=deepseek-v4-flash` |

Debug-level logging includes:
- API request details (model, system prompt, tool count)
- Tool registration info
- Response processing details

## Thinking Block Handling

When the model emits a `thinking` or `redacted_thinking` content block during SSE streaming, the agent loop processes it as follows:

1. **Streaming accumulation**: `thinking` deltas are accumulated via `api.DeltaTypeThinking` events. The full thinking text is built incrementally.
2. **Assistant emission**: When the thinking block completes (`content_block_stop`), the engine emits an `assistant` event (in stream-json mode) containing the complete thinking block as part of the message content.
3. **thinking_tokens events**: During active thinking, periodic `system/subtype: thinking_tokens` system events are emitted (every ~100ms or on `content_block_stop`), carrying `estimated_tokens` (running total) and `estimated_tokens_delta` (increment since last event).
4. **Signature handling**: If the thinking block includes a cryptographic signature (for verification), it is preserved in the assistant event's `signature` field.

```json
{"type":"system","subtype":"thinking_tokens","session_id":"sess_abc","uuid":"uuid-123","estimated_tokens":42,"estimated_tokens_delta":42}
{"type":"assistant","content":[{"type":"thinking","thinking":"Let me analyze...","signature":"sig-abc"}],"message_idx":1}
```

Source: engine loop (thinking delta handling), stream emission layer (event emission).

## Tool Result Overflow Handling

When an MCP tool result exceeds `maxMCPOutputChars` (default 100,000 characters,
configurable via `MCP_MAX_OUTPUT_CHARS`), the engine applies content-type-aware handling:

1. **Text content** (`type: text`): parts are concatenated directly. If the final
   concatenated output exceeds `maxMCPOutputChars`, it is truncated to the first N
   characters with a `[Content truncated: original N chars, showing first N chars]`
   notice appended. Text content is **never** written to a separate file.
2. **Binary content** (`type: image` / `type: blob`): the base64-encoded data is
   decoded and written to `$JENNY_HOME/mcp-tool-output/`. The result returned to
   the model includes an inline reference (e.g., `[image saved to: /path/to/file.png]`)
   rather than the raw bytes. Binary content is always persisted to disk regardless
   of size — truncation applies only to the overall string length after all parts
   are assembled.
3. The result string (potentially truncated text + inline binary paths) is returned
   to the model in the `tool_result` block.

This prevents oversized tool outputs from bloating the message context while
preserving access to the complete binary result on disk.

Source: MCP client (output size limit and binary persistence), constants (home directory).

## Compaction & Retry Caps

### Compaction

Context compaction is triggered when the total token count exceeds a threshold (≈60% of model context window). The engine rewrites the message history by:

1. **Summarizing** earlier turns into a condensed summary message.
2. **Preserving** the last N turns (including the pending tool results) unchanged.
3. **Persisting** a compaction boundary entry to the session transcript. On session resume, this boundary is restored as a `<system-reminder>` user message.

Compaction is only triggered between turns (not mid-stream). See [context-compaction.md](./context-compaction.md) for details.

### Retry Caps

- **API retries**: On transient API failures (5xx, network errors), the API client retries up to `MaxRetries` times (default 10) with exponential backoff (base delay 500ms, max 32s, ±25% jitter).
- **MaxTurns** (`StreamConfig.MaxTurns`): A cost/budget concept checked inside the engine loop before each API call. When exceeded, returns `error_max_turns: limit reached at turn %d`. Default 0 = unlimited.
- **MaxIterations** (`StreamConfig.MaxIterations`, `--max-iterations` CLI flag): An outer loop bound checked by the `RunStream` for-loop. When exceeded, returns `max iterations (%d) exceeded`. Default 0 = unlimited.
- **Max tokens**: When the API returns `stop_reason: max_tokens`, the engine emits `subtype: error_max_tokens` with detailed metadata (`category`, `output_tokens`, `max_output_tokens`, `input_tokens`, `threshold`).
- **Max budget**: When cost exceeds `MaxBudgetUSD`, the loop terminates with `error_budget_exceeded`.

## Related Specifications

| Topic | Spec |
|-------|------|
| Query engine lifecycle | [query-engine.md](./query-engine.md) |
| Stream-json protocol | [stream-json.md](./stream-json.md) |
| CLI flags | [cli.md](./cli.md) |
| API client / tool pairing | [anthropic-api-client.md](./anthropic-api-client.md) |
| Context compaction | [context-compaction.md](./context-compaction.md) |
| Cost tracking | [cost-tracking.md](./cost-tracking.md) |
| Parallel tool execution | [parallel-tool-execution.md](./parallel-tool-execution.md) |
| Message normalization | [message-normalization.md](./message-normalization.md) |
| Session persistence | [session-persistence.md](./session-persistence.md) |
| Secret redaction | [secret-redaction.md](./secret-redaction.md) |
| Tool registry | [tool-registry.md](./tool-registry.md) |
