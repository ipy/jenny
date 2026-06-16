---
title: Core Agent Loop
slug: agent-loop
priority: P0
status: done
spec: complete
code: done
package: internal/agent
defer_to: P3
depends_on:
  - anthropic-api-client
gaps: []
---
# Core Agent Loop

## Overview

The core agent loop implements a minimal viable pipeline for AI-driven tool execution:
- Prompt in → API call with tools → Tool execution → Loop back → Text out

## Architecture

```
User Input → agent.Run() → API Client → Anthropic API
                              ↓
                     stop_reason == "tool_use"?
                    / \
                  Yes                        No
                   | |
            Execute tools Return text
            Send results output
                   |                         |
            Loop back to API Final response
```

## Components

### `internal/agent/loop.go`

The main agent loop that orchestrates the interaction:

1. **Initialization**: Sets up API client, working directory, and initial messages
2. **Tool Conversion**: Converts tool definitions to API format
3. **Main Loop**: Iterates sending messages and processing responses (configurable via `--max-iterations`; default unlimited)
4. **Response Processing**: Handles text and tool_use content blocks
5. **Tool Execution**: Executes requested tools and collects results
6. **Message Building**: Constructs proper message payloads with tool_use blocks and tool_results

### `internal/tool/`

Provides tool implementations:

- **BashTool**: Executes shell commands (read-only by default)
  - Validates commands against read-only allowlist
  - Captures stdout/stderr and exit codes
  - Enforces timeout (default 30s)
- **ReadTool**: Reads files with line numbers
  - Validates paths to prevent traversal attacks
  - Returns content in `cat -n` format
  - Supports offset and limit parameters

### `internal/api/client.go`

Anthropic API client wrapper:
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
        "echo", "date", "which", "type", "file", "stat", "diff",
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

```go
tools := []tool.Tool{
    tool.NewBashTool(),
    tool.NewReadTool(),
}

result, err := agent.Run(ctx, "list the files in the current directory", tools, cwd, 0)
if err != nil {
    // Handle error
}
fmt.Print(result)
```

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
| `--dangerously-skip-permissions` | Skip permission prompts (always allowed in jenny) |
| `-r <session_id>` | Resume an existing session |
| `--mcp-config <path>` | MCP configuration file path (can be specified multiple times) |

### Exit Codes

- 0: Success
- Non-zero: Error (with stderr message)

### Streaming JSON Output (stream-json)

When using `--output-format stream-json`, each output line is a JSON object:

```json
{"type":"message","content":"partial text","session_id":"sess_12345","is_partial":true,"message_idx":0}
{"type":"tool_use","session_id":"sess_12345","tool_name":"bash","tool_input":{"command":"ls"},"message_idx":1}
{"type":"tool_result","session_id":"sess_12345","content":"file1.txt\nfile2.txt","is_error":false,"message_idx":1}
{"type":"result","result":"Final response text","session_id":"sess_12345","model":"deepseek-v4-flash","usage":{"input_tokens":100,"output_tokens":50}}
```

#### Message Types

- `message`: Partial text content (when `--include-partial-messages` is used)
- `tool_use`: Model requested a tool call
- `tool_result`: Tool execution result
- `result`: Final result (last line), includes `model` and `usage` fields

## Configuration

The agent reads configuration from environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `ANTHROPIC_BASE_URL` | API endpoint URL | (none, uses SDK default) |
| `ANTHROPIC_AUTH_TOKEN` | API authentication token | (none, uses SDK default) |
| `ANTHROPIC_MODEL` | Model to use for completions | `deepseek-v4-flash` |
| `JENNY_DEBUG` | Enable debug-level structured logging to stderr | (none) |

Example `.env` file:
```
ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic
ANTHROPIC_AUTH_TOKEN=your-token-here
ANTHROPIC_MODEL=deepseek-v4-flash
```

## Logging

The agent uses Go's `log/slog` for structured logging. All log output is written to stderr to keep stdout clean for agent responses.

| JENNY_DEBUG value | Log level | Example output |
|------------------|-----------|----------------|
| unset | `INFO` and above only | `level=INFO msg="Sending message" model=deepseek-v4-flash` |
| `1` | `DEBUG` and above | `level=DEBUG msg="Sending message" model=deepseek-v4-flash` |

Debug-level logging includes:
- API request details (model, system prompt, tool count)
- Tool registration info
- Response processing details

## Thinking Block Handling

When the model emits a `thinking` or `redacted_thinking` content block during SSE streaming, the agent loop processes it as follows:

1. **Streaming accumulation**: `thinking` deltas are accumulated via `api.DeltaTypeThinking` events. The full thinking text is built incrementally.
2. **Assistant emission**: When the thinking block completes (`content_block_stop`), the engine emits an `assistant` event (in stream-json mode) containing the complete thinking block as part of the message content. See `internal/agent/engine_stream.go`: `emitAssistantEvent`.
3. **thinking_tokens events**: During active thinking, periodic `system/subtype: thinking_tokens` system events are emitted (every ~100ms or on `content_block_stop`), carrying `estimated_tokens` (running total) and `estimated_tokens_delta` (increment since last event). See `internal/agent/engine_stream.go`: `emitThinkingTokens` / `emitThinkingTokensFinal`.
4. **Signature handling**: If the thinking block includes a cryptographic signature (for verification), it is preserved in the assistant event's `signature` field.

```json
{"type":"system","subtype":"thinking_tokens","session_id":"sess_abc","uuid":"uuid-123","estimated_tokens":42,"estimated_tokens_delta":42}
{"type":"assistant","content":[{"type":"thinking","thinking":"Let me analyze...","signature":"sig-abc"}],"message_idx":1}
```

Source: `internal/agent/engine_loop.go` (thinking delta handling), `internal/agent/engine_stream.go` (event emission), `internal/agent/thinking_tokens_test.go` (tests).

## Tool Result Overflow Handling

When an MCP tool result exceeds `maxMCPOutputChars` (default 100,000 characters,
configurable via `MCP_MAX_OUTPUT_CHARS`), the engine applies content-type-aware handling:

1. **Text content** (`type: text`): parts are concatenated directly. If the final
   concatenated output exceeds `maxMCPOutputChars`, it is truncated to the first N
   characters with a `[Content truncated: original N chars, showing first N chars]`
   notice appended. Text content is **never** written to a separate file.
2. **Binary content** (`type: image` / `type: blob`): the base64-encoded data is
   decoded and written to `$JENNY_HOME/mcp-tool-output/` via
   `persistBinaryToolResult` in `internal/mcp/client.go`. The result returned to
   the model includes an inline reference (e.g., `[image saved to: /path/to/file.png]`)
   rather than the raw bytes. Binary content is always persisted to disk regardless
   of size — truncation applies only to the overall string length after all parts
   are assembled.
3. The result string (potentially truncated text + inline binary paths) is returned
   to the model in the `tool_result` block.

This prevents oversized tool outputs from bloating the message context while
preserving access to the complete binary result on disk.

Source: `internal/mcp/client.go` (`maxMCPOutputChars`, `persistBinaryToolResult`,
`executeToolBlock`), `internal/constants/constants.go` (`JennyHomeDir`).

## Compaction & Retry Caps

### Compaction

Context compaction is triggered when the total token count exceeds a threshold (≈60% of model context window). The engine rewrites the message history by:

1. **Summarizing** earlier turns into a condensed `compacted_boundary` system message.
2. **Preserving** the last N turns (including the pending tool results) unchanged.
3. **Emitting** a `system/subtype: compact_boundary` event in stream-json mode with metadata (`trigger`, `pre_tokens`, `preserved_segment`).

Compaction is only triggered between turns (not mid-stream). Source: `internal/agent/compact.go` (`compactMessages`), `internal/agent/engine_loop.go` (compaction trigger).

### Retry Caps

- **API retries**: On transient API failures (5xx, network errors), the API client retries up to `MaxRetries` times (default 10) with exponential backoff (base delay 500ms, max 32s, ±25% jitter). See `internal/api/retry.go`.
- **Max turns**: The agent loop respects `maxTurns` / `MaxIterations` (default 0 = unlimited). When exceeded, the loop terminates with `error_max_turns`.
- **Max tokens**: When the API returns `stop_reason: max_tokens`, the engine emits `subtype: error_max_tokens` with detailed metadata (`category`, `output_tokens`, `max_output_tokens`, `input_tokens`, `threshold`). See `internal/agent/engine_stopreasons.go`: `handleStopMaxTokens`.
- **Max budget**: When cost exceeds `MaxBudgetUSD`, the loop terminates with `error_budget_exceeded`.

## Related Specifications

| Topic | Spec |
|-------|------|
| Stream-json protocol | [stream-json.md](./stream-json.md) |
| CLI flags | [cli.md](./cli.md) |
| API client / tool pairing | [anthropic-api-client.md](./anthropic-api-client.md) |
| Parallel tool execution | [parallel-tool-execution.md](./parallel-tool-execution.md) |
| Message normalization | [message-normalization.md](./message-normalization.md) |
| Session persistence | [session-persistence.md](./session-persistence.md) |
