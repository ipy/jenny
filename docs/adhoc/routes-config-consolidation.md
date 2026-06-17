---
title: Routes Config Consolidation Research
slug: routes-config-consolidation
status: research
date: 2026-06-17
package: internal/api/router
gaps: []
---

# Routes Config Consolidation Research

## Background

The recent koanf migration in `internal/cli/cli.go` consolidated Jenny's per-invocation configuration (CLI flags + `JENNY_*` env vars + `.jenny/config.json`) into a single layer. One thing it intentionally did NOT touch: the multi-provider router, which has its own config file (`~/.jenny/routes.yaml`) parsed via `gopkg.in/yaml.v3` in `internal/api/router/config.go`.

Two parallel config surfaces in one project is a known anti-pattern (multiple files to know about, two parsers, two schemas, two mental models). User direction: **eventually merge to one config file**. This doc is the research step before any code change.

## What the router config actually does

`~/.jenny/routes.yaml` is a **persistent provider/account/profile setup**, not a per-invocation override. The user maintains it once and it drives runtime routing decisions.

Top-level structure (from `internal/api/router/config.go`):

```yaml
providers:
  - name: "deepseek"
    type: "openai"            # protocol: openai | anthropic | gemini
    base_url: "https://..."
    accounts:
      - name: "personal"
        keys: ["sk-..."]      # API keys, inlined in plaintext today
        priority: 1           # lower = higher priority
    models:
      - name: "deepseek-chat"
        tags: ["cheap"]
        priority: 1
        context_window: 64000
        max_output: 4000

profiles:
  default:
    targets:
      - match: { models: ["deepseek:deepseek-chat"] }
      - match: { tags: ["cheap"] }
    routing_mode: "sticky"
    selection_policy: "round_robin"
    retry_policy: { max_retries: 3, backoff: "exponential" }
    allow_fallback: true
```

**Runtime semantics that matter for migration:**

- `keys: []` is a list of API keys per account — round-robin'd on selection, sequential failover on errors.
- `models: []` per provider carries capability metadata (`context_window`, `max_output`, `tags`) that the engine reads to size its compaction and tool-selection logic.
- `profiles: <name>` is a runtime-switchable target chain. The router supports multiple profiles and `SetProfile(name)` is a runtime API.
- `routing_mode`, `selection_policy`, `retry_policy`, `allow_fallback` are policy fields with defaults applied in `applyDefaults` (priority=1, sticky, round_robin, max_retries=5, backoff=exponential, allow_fallback=true).
- The router **merges** env-derived `legacy-*` providers into a YAML config when both exist (spec line 154). This is the backward-compat shim — if no YAML, `SynthesizeConfigFromEnv` is the only source.
- Three-layer routing logic in `router.go` consumes this: sticky retry → key failover → target fallback. None of this changes with a different file format.

## Codebase reality (scope and shape)

The router is a substantial subsystem. Knowing the size and runtime structure matters for any migration estimate.

**File inventory** (`internal/api/router/`):

| File | Lines | Role |
|---|---|---|
| `config.go` | 157 | YAML unmarshal + defaults + env merge |
| `router.go` | 531 | Three-layer routing, sticky sessions, profile switching |
| `sticky.go` | 274 | Sticky client wrapper around router + health-aware `SendMessage` |
| `health.go` | 107 | `HealthRegistry`: consecutive-failure counter, 30s cooldown, key-level health |
| `legacy.go` | 161 | `SynthesizeConfigFromEnv` (legacy env-var path) |
| `router_test.go` | 387 | NewRouter, SelectEndpoint, NextEndpoint, sticky L1/L2/L3 |
| `gaps_test.go` | 215 | `LoadConfig_EnvMergeWithYAML`, 401-no-retry, sticky integration |
| `sticky_integration_test.go` | 296 | End-to-end sticky flow with httptest servers |
| **Total** | **~2000** | |

**Runtime state (independent of config load):**

- `HealthRegistry` tracks endpoint health (3 consecutive failures → 30s cooldown, success clears immediately, additional failure while in cooldown resets the timer). Loaded at `Init` time, mutated by `ReportError` during the session.
- `SessionState` map (keyed by `sessionID`) holds sticky `*ActiveEndpoint` plus `TargetIndex` and `KeyIndex` for the L1/L2/L3 fallbacks. Created on first `SelectEndpoint(sessionID)`, mutated by `NextEndpoint`.
- `rrCounter` is an `atomic.Uint64` for cross-session round-robin. Survives across `SelectEndpoint` calls in the same process.
- All of the above is **independent of how the config got loaded** — a JSON loader with the same struct types leaves them untouched.

**Dependency graph (today):**

```
cmd/jenny/main.go ──► router.Init("") ──► router.LoadConfig ──► yaml.v3
                              │
                              └──► SynthesizeConfigFromEnv ──► os.Getenv
```

`internal/api/router` does **not** depend on `internal/cli`. After the merge it will, because the config struct would be unmarshalled by the CLI koanf instance and passed to `router.Init(cfg *Config)`.

## Why routes.yaml was separate from the rest

- **Persistent vs ephemeral**: `.jenny/config.json` is per-invocation overrides (a model name, a transcript dir); `routes.yaml` is multi-account, multi-profile persistent state the user rarely edits.
- **Schema complexity**: The router config has nested lists of structs (`Provider → Account → Keys`, `Profile → Target → MatchClause`). koanf's `k.Unmarshal` works fine for this shape but the resulting koanf key namespace would be wide (e.g. `providers.0.accounts.0.keys.0`).
- **YAML ergonomics for humans**: The author of routes.yaml was probably avoiding JSON for the same reason Anthropic's API config docs use JSON but most app config uses YAML. Multi-account, multi-model configs are easier to read in YAML.
- **Startup time vs runtime**: The router is `init()`-flavoured — it loads once and the loaded `*Config` lives in a global. CLI flags (per-invocation) are a different concern.

## Health-registry behaviour vs config reload

`HealthRegistry` is constructed at `Init` time and lives for the process lifetime. **It does not depend on the config schema** — it tracks (provider, account, model, key) tuples that the router extracts from `*ActiveEndpoint`, not from the YAML/JSON struct directly.

Implication for migration: switching to `envname` references means the **key string** stored in `HealthRegistry` is the env-resolved value (e.g. `sk-abc...`), not the literal `env:OPENAI_API_KEY` token. Two different env-var names that happen to point to the same real key would still be treated as one endpoint (good). Two different env-var names that resolve to different real keys but used to be the same literal in YAML would suddenly be two endpoints (acceptable, rare in practice).

Env-var resolution happens once at `Init` time. There is no current mechanism to re-resolve mid-session; that is unchanged by this migration.

## `isGenAIEnvSet` heuristic (legacy env-var fallback)

`internal/api/router/legacy.go` defines a 7-way OR check for whether to synthesise a GenAI provider from env vars:

```go
isGenAIEnvSet() → GENAI_API_KEY | GENAI_BASE_URL | GOOGLE_API_KEY |
                  GEMINI_API_KEY | (GOOGLE_CLOUD_PROJECT + REGION) |
                  GOOGLE_GENAI_USE_VERTEXAI
```

This is the "did the user set any GenAI-related env var?" probe. After the merge to one JSON file, this entire path **could** be removed if no JSON file is present and the user is expected to write the file. But removing it would break users who today rely on env vars and don't have a JSON config. The minimum non-breaking change is: keep `SynthesizeConfigFromEnv` as a fallback when no config file is found, the same way it works today for `routes.yaml`. Nothing changes in the legacy env-var path.

## Field-naming style conflict (YAML vs koanf tags)

The router YAML uses `snake_case` for multi-word fields:

```
base_url, context_window, max_output, max_retries,
routing_mode, selection_policy, allow_fallback
```

The existing CLI koanf struct (`internal/cli/cli.go`) uses **kebab-case** for koanf tags:

```go
TranscriptDir          string            `koanf:"transcript-dir"`
MaxToolConcurrency     int               `koanf:"max-tool-concurrency"`
DisableCompact         bool              `koanf:"disable-compact"`
```

If we keep the router struct in Go with idiomatic names (`BaseURL`, `ContextWindow`, `MaxOutput`, etc. — capitals for acronyms per Go style), and the JSON config uses kebab-case, the koanf tags would be:

```go
type Provider struct {
    Name    string  `koanf:"name"`
    Type    string  `koanf:"type"`
    BaseURL string  `koanf:"base-url"`        // JSON: "base-url"
    ...
}
type Model struct {
    ContextWindow int `koanf:"context-window"` // JSON: "context-window"
    MaxOutput     int `koanf:"max-output"`     // JSON: "max-output"
}
```

**This is a breaking format change for any user who has hand-edited `routes.yaml` to match the snake_case keys.** The migration tool (`jenny routes migrate`) must rewrite field names. Recommend documenting the JSON field names in the `migrate` command's `--help` so users can spot-check.

## `ApplyDefaults` and the `*bool` map mutation idiom

`config.go:124-157` (current code) does this:

```go
for name, profile := range cfg.Profiles {
    if profile.RoutingMode == "" { profile.RoutingMode = "sticky" }
    ...
    if profile.AllowFallback == nil {
        defaultAllow := true
        profile.AllowFallback = &defaultAllow
    }
    cfg.Profiles[name] = profile   // write-back
}
```

**Gotcha:** the `profile` loop variable is a **value copy** of the map entry. Mutating its fields doesn't affect the map until you assign back. This works today because `applyDefaults` runs on the result of `yaml.Unmarshal`, where `cfg.Profiles[name]` is a value type (not a pointer). After migration to `k.Unmarshal`, this should still work — the unmarshalled struct is still a value type — but it's worth pinning with a test.

**Second gotcha:** `AllowFallback *bool` is a pointer. When the JSON file omits the field, koanf's unmarshaller leaves the pointer `nil`, and `applyDefaults` correctly sets it to a fresh `&true`. When the JSON has `"allow_fallback": false`, koanf allocates a fresh `&false`. When the JSON has `"allow_fallback": true`, koanf allocates a fresh `&true`. Behaviour matches the YAML path. **No migration hazard here**, but should be covered by an explicit test so a future koanf upgrade doesn't silently break pointer semantics.

## `RetryPolicy` schema/code mismatch (open bug surfaced by research)

The current `multi-provider-routing.md` example (lines 75-80) shows:

```yaml
retry_policy:
  on_rate_limit:
    max_retries: 3
    backoff: "exponential"
  on_server_error:
    max_retries: 2
```

But the actual `RetryPolicy` struct in `config.go:62-66` is:

```go
type RetryPolicy struct {
    MaxRetries int    `yaml:"max_retries"`
    Backoff    string `yaml:"backoff"`
}
```

**No `on_rate_limit` / `on_server_error` sub-structs.** The YAML example is aspirational, not what the code actually unmarshals. The router code in `router.go` only ever reads `RetryPolicy.MaxRetries` (single int), not a layered policy.

This is **out of scope for the routes.yaml → JSON migration** (the migration just moves the schema as-is), but it's worth a separate cleanup: either delete the layered example from `multi-provider-routing.md` or implement the layered `RetryPolicy` as a follow-up.

## Test fixtures and surface

Tests in `internal/api/router/` that pin the YAML file format:

- `gaps_test.go:85-137` — `TestLoadConfig_EnvMergeWithYAML` inlines a YAML string with `providers.accounts.keys` shape.
- `gaps_test.go:131` — `LoadConfig("/nonexistent/path/routes.yaml")` — exercises the "file missing → nil" branch. After migration, this becomes `LoadConfig("/nonexistent/path/config.json")` or the JSON equivalent.
- All `*_test.go` files construct `*Config` literals directly (not via YAML), so they survive the format change as long as the Go struct fields stay the same.

**Migration test plan:**

- `TestLoadConfig_JSON_EnvMerge` — same as `TestLoadConfig_EnvMergeWithYAML` but the input file is JSON. Proves the env-merge path still works through the new loader.
- `TestLoadConfig_EnvNamePrefix` — asserts `keys: ["env:OPENAI_API_KEY"]` resolves via `os.Getenv`. With `t.Setenv("OPENAI_API_KEY", "sk-...")` the resolved key in the parsed `Config` is `sk-...`.
- `TestLoadConfig_EnvNamePrefix_MissingEnvFails` — `keys: ["env:MISSING"]` with env unset must return an error at load time, not a silent empty key.
- `TestApplyDefaults_PreservesExplicitFalse` — JSON `"allow_fallback": false` must survive the defaults pass (the `*bool` semantic pin).

## Multi-provider realistic configs

The single-provider example in this doc is the minimum. Real configurations tend to have 3-5 providers (Anthropic + OpenAI + DeepSeek + Gemini + maybe a local Ollama) and 2-4 profiles (`default` / `vision` / `cheap` / `fast`). A realistic YAML is **150-300 lines**.

If we merge to a single JSON file, the file will be in the same range. That's manageable but it's a real readability regression for users who today have a separate `routes.yaml` because the JSON config also carries per-invocation flags (`--transcript-dir`, `--redact`, etc.). The two file types have different mental models: flags-vs-data.

**Mitigation options to consider:**

- Keep the routes section under a top-level `routes:` key in `~/.jenny/config.json`, so the JSON shape is `{"flags": {...}, "routes": {...}}`. Users can still mentally separate "command-line-style flags" from "data" by reading the right top-level key.
- Or, keep the file split but both go through koanf: per-invocation flags read project `.jenny/config.json`, persistent routes read `~/.jenny/config.json`. The user's "one config file" directive is met (conceptually) but physically there are two — one is per-project overrides, one is global setup. This is closer to a layering decision than a format decision.

## Scope of per-invocation overrides on routes

`Flags.Routes` (if added) needs an explicit scope rule: does a per-invocation `--routes-profile=vision` override the user's persistent default profile? Does `JENNY_ROUTES_PROVIDER=openai` force the use of one provider regardless of routing logic?

If yes, we need:

- `Flags.Routes *router.Config` (full override) or
- `Flags.RoutesProfile string` (one field override) or
- a small set of override fields like `Flags.RoutesProvider string`, `Flags.RoutesProfile string`

**Recommendation:** minimal — `Flags.RoutesProfile string` is enough to satisfy the subagent profile invocation contract from `multi-provider-routing.md:160`. Anything else (forcing a provider, adding a key for one session) can wait until there's a real use case.

## Environment-variable overrides for routes

The CLI layer uses `JENNY_*` env vars (koanf's `env.Provider` strips the prefix and lowercases). If we extend to routes, the natural env-var shape for nested arrays is dotted: `JENNY_ROUTES__PROVIDERS__0__NAME=deepseek`. That's koanf's default for nested keys.

User-friendly alternatives:

- `JENNY_ROUTE_<NAME>=...` — per-provider override. Naming collision with the existing `JENNY_REDACT` style.
- One-shot override: `JENNY_ROUTES_PROFILE=vision`. Easy. Limited.
- No env-var override for routes, only the JSON file. Simplest. Users edit the file once.

**Recommendation:** no env-var override for routes in this round. The JSON file is the canonical surface. Subagent profile selection goes through `Flags.RoutesProfile` (a CLI flag) which already exists conceptually. If a use case emerges for env-var-based route override, add it then — koanf makes the dotted-form env-var provider trivial to enable.

## Constraints from user direction

1. **One config file** — eventually. Phrasing was "现在不做" so this is the plan, not a today-task.
2. **Secrets support both `literal` and `envname`** — instead of `keys: ["sk-..."]` inlining the real key, allow `keys: ["env:OPENAI_API_KEY"]` so the file can be committed safely. Both forms are accepted in the same array.

## Migration options

### Option A: koanf YAML, same file

- Add `github.com/knadh/koanf/parsers/yaml` to the router.
- Replace `yaml.Unmarshal` with `k.Load(file.Provider("~/.jenny/routes.yaml"), yaml.Parser())` + a typed unmarshal of the parsed `k` into the existing `Config` struct.
- File format unchanged. No user-visible disruption. Removes the `gopkg.in/yaml.v3` direct import.
- Scope: ~30 lines of code in `config.go`. Doesn't address the "one config file" goal — `routes.yaml` and `.jenny/config.json` still coexist.

### Option B: One JSON file, merge route definitions in

- Move the YAML schema into a top-level `routes:` key inside `~/.jenny/config.json`.
- The CLI koanf instance loads `~/.jenny/config.json`. Add a second pass that unmarshals the `routes` key into the router `Config` struct.
- **Migration cost**: Real — users with existing `routes.yaml` need a one-time conversion (ship `jenny routes migrate` subcommand).
- **Secret handling**: `keys: ["sk-..."]` literals stay; `keys: ["env:OPENAI_API_KEY"]` resolves at init. Two forms in the same `[]string` array.
- **Conflict with "secrets in JSON"**: the user explicitly allowed `literal` form, so committing plaintext keys to JSON is on the user (same risk as committing them to YAML today).
- **Scope**: ~150-200 lines: file-format change, `jenny routes migrate` subcommand, dual-format read window, doc updates.

### Option C: One JSON file, secret-only via envname

- Strictest form: `keys` ONLY accepts `env:` entries; no literal form.
- Pros: Config is always safe to commit; no `chmod 600` concern.
- Cons: Breaks every existing user. The migration tool can't extract literal values from YAML to push into env vars — it would just error out.
- **Recommendation: not viable as a forced migration**. Could be a future hardening step if all users move secrets to env vars.

## Key design decisions to lock down

### Q1 — Single config file name and location

Locked (line 152 below): `~/.jenny/config.json`. Both per-invocation overrides and persistent routes live there. Project-local `.jenny/config.json` continues to exist for per-project overrides; home is the fallback.

### Q2 — Where the secret/envname syntax lives in the schema

Locked (line 158 below): string-prefix convention in `Account.Keys []string`. `"env:NAME"` resolves at init time; anything else is literal.

### Q3 — Backward compat window for routes.yaml

Locked (line 169 below): hard cutover, no compat read.

### Q4 — Subagent profile invocation

Locked (line 175 below): no API change. `SetProfile(name)` is the same call.

## Decisions (locked 2026-06-17)

### Q1 — Single file location: `~/.jenny/config.json`

Both per-invocation overrides and persistent router setup live in `~/.jenny/config.json`. This is a scope change for the current CLI koanf path (which reads project-local `.jenny/config.json`) — the loader falls back to home if the project file doesn't exist, the same way the router currently does. Order: project `.jenny/config.json` first, then `~/.jenny/config.json` overrides (project wins, same as the existing koanf precedence).

### Q2 — Secret syntax: string-prefix convention

`Account.Keys []string` keeps its current shape. Interpretation rules:

- `"env:OPENAI_API_KEY"` — load via `os.Getenv("OPENAI_API_KEY")` at router init time. Empty result → error at startup (fail fast, no silent empty key in the round-robin pool).
- Anything else — literal API key, used as-is.

The `env:` prefix is the only sentinel. Rationale: zero new types, the existing `[]string` field stays, the loader is a 5-line `if strings.HasPrefix(k, "env:")` check. Trade-off acknowledged: a real API key that happens to start with the literal characters `env:` would be misinterpreted as an env-ref. This is acceptable because:

- No well-known provider issues keys in that form.
- The loader fails fast on a missing env var, so a mis-key would surface immediately.
- The user explicitly preferred this convention.

### Q3 — Backward compat: hard cutover, no `routes.yaml` support

The router stops reading `~/.jenny/routes.yaml` entirely. Users with existing YAML configs get a one-shot `jenny routes migrate` subcommand that converts YAML → JSON (literal keys pass through, no key extraction or guessing). Once the migration command runs successfully, the YAML file is left in place but ignored.

Edge case worth surfacing: if a real API key in the YAML starts with `env:` after migration, the migrated JSON will silently mis-interpret it. The migration command should warn when a key matches the `env:` prefix and require explicit confirmation, or just emit the literal form as `"literal:sk-..."` to disambiguate. Recommend the latter — it preserves the round-trip exactly and a future cleanup can drop the `literal:` prefix once all real keys have been audited.

### Q4 — Subagent profile invocation

No change. `SetProfile(name)` is a router API; the source of profile definitions moves from YAML to JSON but the runtime contract is unchanged. Confirmed no test pins the YAML file path.

## Recommended sequencing

1. **Now (this round)**: stop here. The research is the deliverable.
2. **Next round (small, low-risk)**: Option A — add `koanf/parsers/yaml` to the router, swap `yaml.v3` for the koanf parser, keep `~/.jenny/routes.yaml` as-is. Removes the `gopkg.in/yaml.v3` direct dep. ~30 lines. Gives us a koanf-native path in the router without changing the file format.
3. **Following round (medium, user-facing)**: write `jenny routes migrate <yaml> -> <json>` subcommand. Pure translation: YAML schema → JSON schema (with snake_case → kebab-case field rename), with the `env:` prefix prepended to any key that looks like an env ref, or `literal:` prepended to preserve ambiguous keys. ~200 lines including subcommand plumbing.
4. **Following round (the actual merge)**: extend `internal/cli/cli.go` koanf instance to read `~/.jenny/config.json` as a fallback, add a `RoutesProfile string` field to the `Flags` struct (no `*router.Config` — keep it minimal). Router init takes the unmarshalled `*Config` instead of opening the YAML file directly. ~150 lines + tests + doc updates.
5. **Final round (cutover)**: drop the YAML file read path entirely, ship a release note that `.jenny/routes.yaml` is no longer read.

## Out of scope for this research

- Schema changes to provider/protocol types (e.g. adding OpenAI Responses API fields) — orthogonal.
- Runtime behavior changes (routing algorithm, health/cooldown) — unchanged.
- The `RetryPolicy` layered example in `multi-provider-routing.md` vs single-field `RetryPolicy` struct mismatch — separate cleanup.
- The `SynthesizeConfigFromEnv` legacy path stays as-is until the migration question is settled.
- The `Flags.RoutesProfile` per-invocation override is a one-field stub; richer per-invocation override waits for a real use case.

## What still needs user input before implementation

- Q2.b: confirm the `env:` prefix collision policy. Acceptable options:
  - **(a)** Single prefix `env:` (current plan). Mis-key risk noted above.
  - **(b)** Two prefixes `env:` and `literal:` — `literal:` is optional and only needed to disambiguate when a real key starts with `env:`. Slightly more verbose, no collision.
  - **(c)** Adopt the union-array form (`[{"envname": "..."}]`) after all. More code, zero collision.
- Q3.b: confirm the `jenny routes migrate` behaviour for ambiguous keys (auto-`literal:` prefix, or interactive prompt, or just error out).
- Migration: should `jenny routes migrate` write to `~/.jenny/config.json` directly, or print to stdout and let the user redirect? Direct write is one-shot but loses user control; stdout is scriptable.
- Field-naming: confirm kebab-case in JSON (`base-url`, `context-window`) over snake_case (`base_url`, `context_window`). Kebab matches existing `cli.go` koanf tags; snake matches the current YAML.
- `Flags.RoutesProfile` vs `Flags.Routes *router.Config`: minimal flag stub, or full struct override?
