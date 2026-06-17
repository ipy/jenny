---
title: Routes Config Consolidation Research
slug: routes-config-consolidation
status: completed
date: 2026-06-17
package: internal/api/router
gaps: []
---

# Routes Config Consolidation Research

## Background

The recent koanf migration in `internal/cli/cli.go` consolidated Jenny's per-invocation configuration (CLI flags + `JENNY_*` env vars + `.jenny/config.json`) into a single layer. One thing it intentionally did NOT touch: the multi-provider router, which has its own config file (`~/.jenny/routes.yaml`) parsed via `gopkg.in/yaml.v3` in `internal/api/router/config.go`.

Two parallel config surfaces in one project is a known anti-pattern (multiple files to know about, two parsers, two schemas, two mental models). **Final Goal: Consolidate all persistent configuration into a single `config.json` file immediately.** We will drop support for `routes.yaml` and unify the file-based configuration surface.

## What the router config actually does

`~/.jenny/routes.yaml` (legacy) is a **persistent provider/account/profile setup**, not a per-invocation override. In the new unified model, these definitions move to a top-level `routes` key in `config.json`.

**New Unified JSON Structure Example:**

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
            "keys": ["env:DEEPSEEK_API_KEY", "sk-literal-key"],
            "priority": 1
          }
        ],
        "models": [
          {
            "name": "deepseek-chat",
            "tags": ["cheap"],
            "priority": 1,
            "context-window": 64000,
            "max-output": 4000
          }
        ]
      }
    ],
    "profiles": {
      "default": {
        "targets": [
          { "match": { "models": ["deepseek:deepseek-chat"] } },
          { "match": { "tags": ["cheap"] } }
        ],
        "routing-mode": "sticky",
        "selection-policy": "round_robin",
        "retry-policy": { "max-retries": 3, "backoff": "exponential" },
        "allow-fallback": true
      }
    }
  }
}
```

**Runtime semantics that matter for consolidation:**

- `keys: []`: List of API keys per account. Interpretation rules:
    - `"env:NAME"`: Resolved via `os.Getenv("NAME")` at router init time. Empty result → error at startup.
    - `"literal:VALUE"`: Interpreted as the literal key `VALUE`.
    - `"VALUE"` (unprefixed): Interpreted as the literal key `VALUE`.
- `models: []`: Metadata for compaction and tool-selection.
- `profiles`: Target chains and policies (`sticky`, `round_robin`, etc.).
- `ApplyDefaults`: Priority=1, sticky, round_robin, max_retries=5, backoff=exponential, allow_fallback=true.

## Dual Configuration Surfaces

Jenny supports two primary ways to configure providers:

1.  **File-Based (`config.json`)**: Persistent, multi-account, and multi-profile setup.
2.  **Environment-Based (Zero-Config)**: Support for standard variables like `ANTHROPIC_AUTH_TOKEN`, `OPENAI_API_KEY`, etc. This is a **necessary and first-class mechanism** for quick setup and CI/CD environments.

**The "Consolidation" specifically addresses the file surface:** moving from YAML (`routes.yaml`) to JSON (`config.json`).

## Codebase reality (scope and shape)

The router is a substantial subsystem. Knowing the size and runtime structure matters for the implementation.

**File inventory** (`internal/api/router/`):

| File | Lines | Role |
|---|---|---|
| `config.go` | 157 | Struct definitions + defaults + secret resolution |
| `router.go` | 531 | Three-layer routing, sticky sessions, profile switching |
| `sticky.go" | 274 | Sticky client wrapper around router + health-aware `SendMessage` |
| `health.go` | 107 | `HealthRegistry`: consecutive-failure counter, 30s cooldown |
| `legacy.go` | 161 | **Refactored**: Zero-Config Environment Provider logic |
| `router_test.go` | 387 | Routing logic tests |
| `gaps_test.go` | 215 | Integration tests (updated for JSON) |
| `sticky_integration_test.go` | 296 | E2E sticky flow tests |
| **Total** | **~2000** | |

**Dependency graph (Final State):**

```
cmd/jenny/main.go ──► CLI Koanf ──► router.Init(cfg)
                              │
                              └──► config.json
```

`internal/api/router` will no longer handle its own file I/O or YAML parsing. It will receive a pre-populated `*Config` struct from the CLI layer's `koanf` instance.

## Health-registry behaviour

`HealthRegistry` is constructed at `Init` time and lives for the process lifetime. It tracks (provider, account, model, key) tuples.
**Secret Resolution:** Resolution happens once at `Init` time. The key string stored in `HealthRegistry` will be the **resolved** value (e.g., `sk-abc...`), not the `env:VAR` token. This ensures health tracking is accurate and independent of the configuration source.

## Environment Variable Synthesis (Zero-Config)

When no file-based configuration is provided (or as a supplement), the router automatically synthesizes provider configurations from standard environment variables. This is **not legacy**; it is a core feature for ease of use.

**Supported Synthesis Variables:**
- **OpenAI**: `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL`
- **Anthropic**: `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL`
- **Gemini/Vertex**: `GENAI_API_KEY`, `GOOGLE_API_KEY`, `GEMINI_API_KEY`, etc.

**Priority Rule:**
If a `config.json` exists, its definitions are loaded first. Environment-synthesized providers are then appended/merged. This allows a user to have a stable `config.json` for most models but quickly override or add a provider via a single env-var.

## Field-naming: Kebab-case

The router YAML used `snake_case`. The CLI koanf struct (`internal/cli/cli.go`) uses **kebab-case** for koanf tags.
**Decision:** All keys in `config.json` (including the `routes` section) will use **kebab-case** to maintain consistency across the entire project configuration.

```go
type Provider struct {
    BaseURL string `koanf:"base-url"`
}
type Model struct {
    ContextWindow int `koanf:"context-window"`
}
```

## `ApplyDefaults` and Pointer Semantics

The existing `ApplyDefaults` logic for `*bool` fields (like `AllowFallback`) will be preserved. Koanf's unmarshaller handles pointer types correctly, allowing us to distinguish between an omitted field (defaults to `true`) and an explicit `false`.

## Decisions (Locked)

### Q1 — Single file location: `~/.jenny/config.json`

Both per-invocation overrides and persistent router setup live in `~/.jenny/config.json`. Project-local `.jenny/config.json` continues to exist for per-project overrides.

### Q2 — Secret syntax: Prefix-based resolution

`Account.Keys []string` interpretation rules:

- `"env:NAME"` — load via `os.Getenv("NAME")` at router init time. Empty result → error at startup.
- `"literal:VALUE"` — use `VALUE` as the literal API key.
- `"VALUE"` (unprefixed) — use `VALUE` as the literal API key.

### Q3 — Backward compatibility: None

- `routes.yaml` is no longer supported.
- The term "legacy" is removed from environment synthesis; it is now the "Zero-Config" path.
- All file-based configuration must be in `config.json`.

### Q4 — Subagent profile invocation

No change to the `SetProfile(name)` API. `Flags.RoutesProfile` will be added to the CLI layer to allow per-invocation profile selection via CLI flags or `JENNY_ROUTES_PROFILE` env vars.

### Q5 — `LoadConfigFromKoanf` function signature

`LoadConfigFromKoanf(k *koanf.Koanf) (*Config, error)` unmarshals the `routes` key from the provided koanf instance into a `*Config`. If no `routes` key is present, returns `&Config{Profiles: make(map[string]Profile)}` (never nil, so callers can call `applyDefaults` and `mergeEnvProviders` unconditionally). After unmarshalling, merges env-synthesized providers and applies defaults before returning.

## Implementation Plan

1.  **Refactor Router Config**: Update `internal/api/router/config.go` with `koanf` tags (kebab-case). Implement the `ResolveKeys()` method.
2.  **Standardize Environment Synthesis**: Rename `legacy.go` to `environment.go` (or similar) and refine the logic to be a first-class "Zero-Config" path.
3.  **CLI Integration**: Update `internal/cli/cli.go` to unmarshal the `routes` key from the global `koanf` instance into the router's `Config` struct.
4.  **Initialization**: Update `cmd/jenny/main.go` to pass the unmarshalled `Config` to `router.Init()`.
5.  **Test Updates**: Update integration tests in `internal/api/router` to use the new JSON-based loading path and verify secret resolution.
6.  **Documentation**: Update all user-facing documentation to point to the single `config.json` format.
