---
title: Secret Redaction
slug: secret-redaction
priority: P3
status: done
spec: complete
code: done
package: internal/redact
gaps: []
---

# Secret Redaction

## Overview

Jenny reads files, runs commands, and fetches URLs. Any tool result may contain API keys, tokens, or passwords that should never reach the LLM. This feature adds a runtime redactor (enabled by default) backed by a **rule-based detector** that mirrors `github.com/zricethezav/gitleaks/v8`'s detection model — rule set, keyword prefilter, per-rule entropy, allowlist, stop words — without taking a runtime dependency on the gitleaks package. Specifically:

- Scans tool results for secrets
- Replaces detected secrets with `[REDACTED:<hex>]` placeholders
- Stores originals in-memory for later recovery (in `recover` mode)
- Recovers original values when the LLM references placeholders

## Security Model

- **In-memory only**: Redacted values are never persisted to disk
- **Default enabled**: Redaction is active by default in `recover` mode.
- **Modes**:
    - `recover`: Redacts secrets and allows recovery in tool inputs (default).
    - `redact`: Redacts secrets but does NOT allow recovery (one-way).
    - `disabled`: Disables redaction entirely.
- **Configuration**: Use `JENNY_REDACT` env var or `-ff redact=<mode>` CLI flag.
- **LLM instruction**: System prompt instructs LLM to preserve placeholder format

## API Reference

### SecretRedactor

```go
package redact

// SecretRedactor detects and redacts secrets in tool results.
type SecretRedactor struct {
    // private fields
}

// RedactMode defines the behavior of secret redaction.
type RedactMode string

const (
    ModeDisabled RedactMode = ""
    ModeRedact   RedactMode = "redact"
    ModeRecover  RedactMode = "recover"
)

// NewSecretRedactor creates a new SecretRedactor.
func NewSecretRedactor(mode RedactMode) *SecretRedactor

// Enabled returns whether redaction is active.
func (r *SecretRedactor) Enabled() bool

// Redact scans content for secrets and replaces them with placeholders.
// Returns the content with all detected secrets replaced.
func (r *SecretRedactor) Redact(content string) string

// Recover replaces placeholders with their original values.
// Unknown placeholders are left unchanged. Only works in ModeRecover.
func (r *SecretRedactor) Recover(content string) string

// Reset clears all stored mappings and resets the counter.
func (r *SecretRedactor) Reset()
```

### Placeholder Format

Placeholder format: `[REDACTED:<hex>]` where `<hex>` is a random 8-character hex string (e.g., `[REDACTED:a3f1b2c9]`).

- Same secret text → same placeholder ID
- Different secrets → different IDs

## Engine Integration

### Tool Result Redaction

Tool results are redacted before being sent to the model. When the redactor is enabled, the result content is passed through the redactor, which replaces detected secrets with placeholders.

### Tool Input Recovery

Tool inputs are recovered before execution. When the redactor is in recover mode, placeholder values in tool input JSON are replaced with their original secret values before the tool is invoked.

### System Prompt Instruction

When enabled, the following is appended to the system prompt:

```
This session has secret redaction enabled. Tool results may contain `[REDACTED:<hex>]` placeholders (e.g. `[REDACTED:a3f1b2c9]`). Copy them verbatim — including the full hex suffix — and never simplify, abbreviate, or otherwise modify them.
```

If the mode is `recover` (the default), this sentence is also appended:

```
They will be automatically recovered when you use them in tool calls, so you can refer to them directly as needed.
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JENNY_REDACT` | `recover` | Set to `disabled`, `redact`, or `recover`. |

### CLI Flags

Use the feature flag mechanism:
- `-ff redact=disabled`
- `-ff redact=redact`
- `-ff redact=recover`

### StreamConfig Field

```go
type StreamConfig struct {
    // ... existing fields ...
    RedactMode redact.RedactMode
}
```

## Detection Rules

The redactor uses a **rule-based detector** that mirrors `gitleaks/v8`'s
detection model in shape and capability level, without importing the
gitleaks package. The detector runs each rule from `defaultRules()` against
the content, gated by keyword prefiltering, per-rule entropy, allowlist, and
stop words — exactly the same machinery gitleaks uses internally.

### Detector shape (mirrors `gitleaks/v8/detect.Detector`)

```go
type Detector struct { rules []Rule }

type Rule struct {
    ID          string
    Description string
    Regex       *regexp.Regexp
    Entropy     float64         // 0 = no entropy check
    Keywords    []string        // empty = no prefilter
    SecretGroup int             // 0 = full match, n = capture group n
    Allowlist   *Allowlist
}

type Allowlist struct {
    Regexes   []*regexp.Regexp
    StopWords []string
}
```

### Default rule set (gitleaks-aligned)

`defaultRules()` returns a 25-rule set covering the most common vendor
tokens plus a generic high-entropy fallback. Each rule mirrors the
corresponding gitleaks default rule in shape (regex, entropy, keywords,
allowlist):

**Vendor-specific (high-signal, narrow regex):**

- `aws-access-token` — `AKIA[0-9A-Z]{16}`
- `aws-secret-key` — context-anchored `aws_secret_key = "..."` (entropy 3.5)
- `stripe-access-token` — `sk_live_...` / `sk_test_...` / `rk_live_...`
- `github-pat`, `github-oauth`, `github-app-token`,
  `github-fine-grained-pat`, `github-refresh-token` — all `gh*_` variants
- `gitlab-pat`, `gitlab-runner-token`
- `slack-token` (`xox[baprs]-`), `slack-webhook`
- `discord-token`, `discord-webhook`
- `openai-api-key` (`sk-...`, `sk-proj-...`)
- `anthropic-api-key` (`sk-ant-...`)
- `npm-token` (`npm_...`)
- `pypi-token` (`pypi-AgEIcHlwaS5vcmc...`)
- `heroku-api-key` — context-anchored UUID
- `twilio-api-key` (`SK[0-9a-f]{32}`)
- `sendgrid-api-key` (`SG....`)
- `mailgun-api-key` (`key-...`)
- `jwt` — `eyJ...eyJ...` three-segment JWT (entropy 3.5)
- `ssh-private-key` — PEM `BEGIN/END` block

**Generic high-entropy fallback:**

- `generic-api-key` — `(?:^|[^0-9A-Za-z_])([A-Za-z0-9_\-]{32,45})(?:[^0-9A-Za-z_\-]|$)`,
  entropy 3.5, **keyword-gated** by `key`, `secret`, `token`, `password`,
  `auth`, `credential`, `bearer`, `api`, `private`, etc. This is the rule
  that catches tokens the vendor-specific rules miss. The keyword
  prefilter is what makes it stable — it only fires on text that *looks*
  like a secret assignment, not on arbitrary high-entropy strings.

### Detection algorithm

For each rule, in order:

1. **Keyword prefilter.** If the rule has keywords, build a set of which
   keywords appear in the content (lowercased substring search, O(n·k) for
   small k). Skip the rule if no keyword is present.
2. **Regex match.** Run the rule's regex against the full content.
3. **Secret extraction.** Take the full match (SecretGroup=0) or a named
   capture group (SecretGroup>0).
4. **Allowlist check.** If the rule has an allowlist and the secret
   matches an allowlist regex OR contains an allowlist stop word, skip
   the finding.
5. **Entropy check.** If the rule has an entropy threshold and the
   secret's Shannon entropy is ≤ threshold, skip the finding.
6. **Emit Finding** with RuleID, Secret, Match, Start, End, Entropy.

### Shannon entropy (referenced from gitleaks)

`shannonEntropy(data)` is mirrored from gitleaks' unexported helper
(the function is inlined with a comment noting the source). All entropy-gated rules call this helper.

### Why reference, not import?

The gitleaks Shannon entropy helper is unexported, so it cannot be imported directly. Running the full gitleaks Detector would pull in many transitive dependencies (viper, aho-corasick, etc.) and hundreds of source-code-oriented rules that aren't needed. Reimplementing the rule-based detection model in-package keeps the binary lean and the behavior auditable, while matching gitleaks' detection capability level.

## Out of Scope

- Persistent storage of redacted values
- Custom rules or patterns (gitleaks defaults only)
- Audit logging
- Streaming output redaction
- Transcript redaction
- CLI flag (env var only)
- Web search, MCP, or subagent result redaction