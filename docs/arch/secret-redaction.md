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

Jenny reads files, runs commands, and fetches URLs. Any tool result may contain API keys, tokens, or passwords that should never reach the LLM. This feature adds a runtime redactor (enabled by default) that **references** `github.com/zricethezav/gitleaks/v8`'s shannon-entropy algorithm and detection conventions — but does **not** import the gitleaks package as a runtime dependency. The formula is mirrored in-package with attribution so the binary stays lean. Specifically:

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

The redactor uses two complementary detection layers. Both layers follow gitleaks'
detection conventions without being a runtime import of the gitleaks package.

### 1. Shannon entropy (gitleaks-referenced)

Tokenizes the content into runs of `[A-Za-z0-9+/=_\-]{20,}` and scores each run
with the Shannon entropy formula from `github.com/zricethezav/gitleaks/v8/detect/utils.go`.
The gitleaks package keeps this helper unexported, so the redact package mirrors
its body verbatim (a ~10-line function) with a `// Copied verbatim from ...`
attribution comment. This means we **reference** gitleaks' algorithm — we don't
import it as a dependency.

A run is treated as a candidate secret when **all** of the following hold:

- **Length:** at least 20 characters.
- **Entropy:** Shannon entropy > `4.5` bits/char (matches gitleaks' per-rule
  default).
- **Character class mix:** contains at least one ASCII digit AND at least one
  ASCII letter. This is the "digit+alpha gate", which mirrors gitleaks'
  treatment of "generic" rules (see `containsDigit` in gitleaks'
  `detect/utils.go`). It exists to avoid redacting long alphabetic or
  numeric-only runs that have high entropy but are not secrets — prose,
  identifiers, repeated patterns, hex-only hashes, ID sequences.

When all three gates pass, the run is replaced with a `[REDACTED:ID_XXXXX]`
placeholder and recorded for later recovery.

### 2. Regex patterns (`additionalPatterns`)

A small set of regex patterns runs as the second layer, used as a precise
fallback for secret prefixes that entropy may miss (short keys, formats with
hyphens/underscores that don't trip the digit+alpha gate as cleanly):

- OpenAI API key (`sk-...`)
- GitHub PAT (`ghp_...`, `gho_...`, `ghr_...`, `github_pat_...`)
- AWS Access Key ID (`AKIA...`)
- AWS Secret Access Key (40-char context-anchored)
- Slack token (`xox[baprs]-...`)
- NPM token (`npm_...`)
- PyPI token (`pypi_...`)
- SSH private keys (PEM `BEGIN/END` block)

Regex runs second so structured secrets are caught even if the entropy layer
flagged the surrounding text differently. Entropy is applied first so long,
prefix-less secrets are caught even when no regex matches.

### Why reference, not import?

`github.com/zricethezav/gitleaks/v8/detect.shannonEntropy` is **unexported**
(lowercase 's'), so it cannot be imported directly. The remaining public
surface — `detect.NewDetectorDefaultConfig()` + `DetectString()` — is
available, but running the full gitleaks Detector would pull in viper,
aho-corasick, semgroup, zerolog, lipgloss, charmbracelet, gitdiff and ~30
transitive dependencies, plus hundreds of source-code-oriented rules we
don't need. The shannon-entropy function (and the digit+alpha gate
convention) is the only piece of gitleaks' signal we actually want for
tool-result redaction. Mirroring keeps the binary lean and the behavior
auditable.

## Out of Scope

- Persistent storage of redacted values
- Custom rules or patterns (gitleaks defaults only)
- Audit logging
- Streaming output redaction
- Transcript redaction
- CLI flag (env var only)
- Web search, MCP, or subagent result redaction