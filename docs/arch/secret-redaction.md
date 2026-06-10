---
title: Secret Redaction
slug: secret-redaction
priority: P3
status: complete
spec: complete
code: complete
package: internal/redact
gaps: []
---

# Secret Redaction

## Overview

Jenny reads files, runs commands, and fetches URLs. Any tool result may contain API keys, tokens, or passwords that should never reach the LLM. This feature adds a runtime redactor (enabled by default) backed by `github.com/zricethezav/gitleaks/v8` that:

- Scans tool results for secrets
- Replaces detected secrets with `[REDACTED:ID_XXXXX]` placeholders
- Stores originals in-memory for later recovery
- Recovers original values when the LLM references placeholders

## Security Model

- **In-memory only**: Redacted values are never persisted to disk
- **Default enabled**: Redaction is active unless `JENNY_REDACT_DISABLE=1` is set
- **LLM instruction**: System prompt instructs LLM to preserve placeholder format

## API Reference

### SecretRedactor

```go
package redact

// SecretRedactor detects and redacts secrets in tool results.
type SecretRedactor struct {
    // private fields
}

// NewSecretRedactor creates a new SecretRedactor.
// Enabled by default unless JENNY_REDACT_DISABLE=1 is set.
func NewSecretRedactor() *SecretRedactor

// Enabled returns whether redaction is active.
func (r *SecretRedactor) Enabled() bool

// Redact scans content for secrets and replaces them with placeholders.
// Returns the content with all detected secrets replaced.
func (r *SecretRedactor) Redact(content string) string

// Recover replaces placeholders with their original values.
// Unknown placeholders are left unchanged.
func (r *SecretRedactor) Recover(content string) string

// Reset clears all stored mappings and resets the counter.
func (r *SecretRedactor) Reset()
```

### Placeholder Format

Placeholder format: `[REDACTED:ID_XXXXX]` where `XXXXX` is a zero-padded 5-digit counter (e.g., `ID_00001`).

- Same secret text → same placeholder ID
- Different secrets → different IDs
- Counter increments across calls

## Engine Integration

### Tool Result Redaction

In `engine_loop.go`, tool results are redacted before being sent to the model:

```go
// Line ~640: before appending to toolResults
if e.secretRedactor != nil && e.secretRedactor.Enabled() {
    emitContent = e.secretRedactor.Redact(emitContent)
}
```

### Tool Input Recovery

In `engine_loop.go`, tool inputs are recovered before execution:

```go
// Line ~580: before executor.Execute
if e.secretRedactor != nil && e.secretRedactor.Enabled() {
    for i, block := range execBlocks {
        if inputJSON, err := json.Marshal(block.Input); err == nil {
            recovered := e.secretRedactor.Recover(string(inputJSON))
            var ri map[string]any
            if err := json.Unmarshal([]byte(recovered), &ri); err == nil {
                execBlocks[i].Input = ri
            }
        }
    }
}
```

### System Prompt Instruction

When enabled, the following is appended to the system prompt:

```
This session has secret redaction enabled. Tool results may contain `[REDACTED:ID_XXXXX]` placeholders. Do not alter, remove, or expand these placeholders.
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JENNY_REDACT_DISABLE` | unset (enabled) | Set to `1` to disable secret redaction |

### StreamConfig Field

```go
type StreamConfig struct {
    // ... existing fields ...
    RedactEnabled bool // Set based on JENNY_REDACT_DISABLE
}
```

## Detection Rules

Uses gitleaks default configuration for detecting:

- OpenAI API keys (`sk-...`)
- GitHub tokens (`ghp_...`, `github_pat_...`)
- AWS keys (`AKIA...`)
- SSH private keys
- And many other common secret patterns

## Out of Scope

- Persistent storage of redacted values
- Custom rules or patterns (gitleaks defaults only)
- Audit logging
- Streaming output redaction
- Transcript redaction
- CLI flag (env var only)
- Web search, MCP, or subagent result redaction