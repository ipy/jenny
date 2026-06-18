---
title: Multi-Provider Routing
slug: multi-provider-routing
priority: P2
status: done
spec: complete
code: done
package: internal/api, internal/agent
depends_on:
  - anthropic-api-client
  - message-normalization
  - rate-limit-handling
---

# Multi-Provider Routing

Specifications for high-availability routing across multiple LLM providers, models, and API keys. This system ensures service continuity, cost optimization, and capability-based routing (e.g., vision) while preserving Prompt Caching through session stickiness.

## Design Goals

- **High Availability**: Automatic fallback when a provider is down or a key is rate-limited.
- **Cost Optimization**: Prefer low-cost models (e.g., DeepSeek) for standard tasks.
- **Capability Routing**: Seamlessly switch to specialized models (e.g., Claude 3.5 Sonnet) for vision or high-reasoning tasks.
- **Cache Preservation**: Maintain session stickiness to a single model/provider to maximize Prompt Caching efficiency.
- **Load Balancing**: Cross-session round-robin for new sessions; sequential key failover within an account on errors.

## Configuration

All routing configuration lives in a `routes` key within `config.json`, loaded through the standard koanf layering (CLI > env > config file). All field names use **kebab-case** for consistency with the rest of the configuration surface.

The configuration is split into **Resource Definitions** (Providers) and **Execution Policies** (Profiles).

### 1. Resource Definitions (Providers)

Defines available backends, models, and credentials.

```json
{
  "routes": {
    "providers": [
      {
        "name": "deepseek",
        "type": "openai",
        "base-url": "https://api.deepseek.com",
        "accounts": [
          {
            "name": "personal",
            "keys": ["env:DEEPSEEK_API_KEY", "sk-ds-2"],
            "priority": 1
          }
        ],
        "models": [
          {
            "name": "deepseek-chat",
            "tags": ["cheap", "text"],
            "priority": 1,
            "context-window": 64000,
            "max-output": 4000
          }
        ]
      }
    ]
  }
}
```

### Secret Resolution

API keys in `accounts[].keys` support three forms:

| Form | Behavior |
|------|----------|
| `env:NAME` | Resolved via environment variable at init time; empty result is a startup error |
| `literal:VALUE` | Interpreted as the literal key `VALUE` |
| `VALUE` (unprefixed) | Interpreted as the literal key `VALUE` |

### 2. Execution Policies (Profiles)

Defines how resources are selected and used.

```json
{
  "routes": {
    "profiles": {
      "default": {
        "targets": [
          { "match": { "models": ["deepseek:deepseek-chat"] } },
          { "match": { "tags": ["cheap"] } }
        ],
        "routing-mode": "sticky",
        "selection-policy": "round_robin",
        "retry-policy": {
          "max-retries": 3,
          "backoff": "exponential"
        },
        "allow-fallback": true
      }
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `routing-mode` | `sticky` (lock to one endpoint per session, protects cache) or `balanced` (re-evaluate per turn) |
| `selection-policy` | `round_robin` (load balance across sessions) or `random` |
| `retry-policy` | Retry count and backoff strategy for current-endpoint failures |
| `allow-fallback` | Whether to switch to the next target when retries are exhausted |

## Routing & Fallback Logic

The routing engine operates on a three-layered recovery logic to balance availability and cache efficiency.

### Layer 1: Sticky Retry (Preserve Cache)
- **Condition**: Rate limit, server overload, server error, or timeout (see [rate-limit-handling](./rate-limit-handling.md) for category-driven decisions).
- **Action**: Backoff and retry on the **same Key and Model**.
- **Goal**: Protect the existing Prompt Cache on the provider's side.
- **Retry schedule**: Category-aware backoff with per-category base and max delays (see [rate-limit-handling](./rate-limit-handling.md)). Default schedule is exponential backoff starting at 500ms, capped at 32s, with ±25% jitter. `Retry-After` header takes precedence when present.
- **Retry count**: Up to `max-retries` retries (default 5), giving 6 total attempts (1 original + 5 retries).

### Layer 2: Key Failover (Preserve Cache)
- **Condition**: Layer 1 retries exhausted, or quota/payment errors (permanent for the current key).
- **Action**: Switch to the next available Key/Account for the **same Model**.
- **Goal**: Maintain stickiness to the model. Caches are often shared across different keys for the same model/provider.
- **Behavior**: Sequential scan of account keys, skipping the current key and any keys in cooldown. Each key is tried once before declaring the model exhausted.

### Layer 3: Model Fallback (Sacrifice Cache)
- **Condition**: All keys for the current model are exhausted, or a model-not-found error occurs.
- **Action**: Move to the next `match` entry in the Profile's `targets` list.
- **Goal**: Ensure task completion at the cost of cache efficiency. Once a fallback occurs, the session locks (sticky) to the new endpoint.
- **Behavior when all targets are exhausted**: Returns a terminal error. The last-failed endpoint's health is recorded before returning.

### Non-Retryable Errors

Certain error categories bypass the retry/fallback layers entirely:

- **Content filter**: Returned immediately as a terminal error; no retry.
- **Auth / Permission**: Returned immediately as a terminal error; no retry.
- **Context exhausted**: Returned to the engine for compaction handling (not retried at the routing layer).

## Load Balancing

Two-tier approach:

- **Cross-session (round-robin)**: New sessions are distributed across matching candidates via a global counter. Distributes load evenly across the pool.
- **Intra-account (sequential failover)**: Keys within an account are tried sequentially on failure (L2), tracking position to avoid re-selecting exhausted keys.

`routing-mode: "balanced"` bypasses sticky session cache and re-evaluates from the candidate pool each call.

## Health & Cooldown

The `HealthRegistry` tracks endpoint health with a consecutive-failure counter:

| Field | Default |
|-------|---------|
| Consecutive failure threshold | 3 failures |
| Cooldown duration | 30 seconds |
| Success clears | Failure count → 0, cooldown removed immediately |
| Cooldown refresh | Each additional failure while in cooldown resets the timer |

When a key reaches 3 consecutive failures it enters a 30-second cooldown. During cooldown `IsHealthy` returns `false` for that key. A single successful call clears all failure state and the cooldown timer.

## Default Values

When a field is omitted from the configuration, the following defaults apply:

| Field | Default Value |
|-------|---------------|
| `priority` (Account/Model) | `1` |
| `routing-mode` | `sticky` |
| `selection-policy` | `round_robin` (within same priority) |
| `max-retries` | `5` |
| `backoff` | `exponential` |
| `allow-fallback` | `true` |

## Zero-Config Environment Synthesis

When no file-based `routes` configuration is present (or as a supplement), the router automatically synthesizes provider configurations from standard environment variables. This is a **first-class mechanism** for quick setup and CI/CD environments, not a legacy fallback.

### Supported Variables

| Provider | Environment Variables | Synthesized Provider Name |
|----------|----------------------|---------------------------|
| Anthropic | `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL` | `anthropic` |
| OpenAI | `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | `openai` |
| GenAI/Gemini | `GENAI_API_KEY`, `GOOGLE_API_KEY`, `GEMINI_API_KEY`, `GENAI_MODEL`, `GEMINI_MODEL`, `GENAI_BASE_URL`, plus Vertex AI signals | `genai` |

### Precedence

If `config.json` contains a `routes` block, those definitions load first. Environment-synthesized providers are appended only when no provider with the same name already exists. This allows a stable `config.json` for most models while quickly adding or overriding a provider via a single environment variable.

## Profile Selection

The active routing profile defaults to `default`. Subagents can specify an alternative profile to trigger specialized routing (e.g., vision-capable or cost-optimized) while inheriting the main session's context.
