---
title: External Model Registry Fetch and Local Cache
slug: external-model-registry
status: proposed
date: 2026-06-19
updated: 2026-06-19
package: internal/config
gaps:
  - jenny has no external source of truth for model capabilities, pricing, and limits
  - max-tokens-clamp proposal requires a capability table that is currently hand-maintained and incomplete
  - cost tracking is hard-coded per model and cannot track new releases
  - DeepSeek V4 limits are stale by 47× (8192 vs actual 384000) — illustrates urgency of external data
depends_on:
  - provider-architecture
  - max-tokens-clamp
---

# External Model Registry Fetch and Local Cache

## Problem

Jenny currently has no external source of truth for model capabilities, output limits, context windows, or pricing. The information that exists lives in three places, each with its own failure mode:

1. **`modelMaxOutputTokens` in `provider_anthropic.go`** covers only two `deepseek-v4-*` model strings with **stale values** (8192 vs. actual 384000, a 47× undercount). Every other model falls into a single default bucket (20000 for Anthropic). The function is hand-maintained and lags behind new releases by months.
2. **Per-provider default `maxTokens` constants** in the four provider constructors are scattered, undocumented, and disagree with the capability table for the same model (Anthropic default 32000 vs. capability default 20000 — the central defect of the [`max-tokens-clamp`](./max-tokens-clamp.md) proposal). Current Claude models actually support 64000–128000 output tokens; both defaults are below the real capability.
3. **Cost tracking** in `internal/agent/cost_state.go` is hard-coded per model. New models are silently billed at zero cost unless someone adds a lookup entry. Pricing drift from providers is invisible until a user notices a wrong bill.

The community-maintained registry at `https://raw.githubusercontent.com/ImSingee/aidy-models/master/models.json` already solves all three problems. It contains 6761+ model entries across 190+ providers with `contextWindow`, `maxOutput`, `pricing`, `modalities`, `abilities`, and `family` fields populated at 79%–100% coverage. The file is ~5.5MB. The registry is MIT-licensed, actively maintained (created March 2026), and merges data from [models.dev](https://github.com/anomalyco/models.dev) and [LobeHub](https://github.com/lobehub/lobehub) with manual corrections.

An alternative registry — [`atzamis/ai-model-directory`](https://github.com/atzamis/ai-model-directory) — provides 7000+ models via GitHub Actions auto-refresh with provider API fetching. It uses TOML source files and generates `all.json`. This could serve as a secondary validation source or a future replacement if the primary registry becomes unmaintained.

The change required is not "build a registry" — that work has been done. The change is: **fetch this file on first start, cache it in jenny's home directory, parse it in parallel with other startup work, and degrade gracefully when the fetch fails or the cache is missing.**

## Design

### Storage

Two files under `~/.jenny/`, each with a single responsibility, plus an embedded section in the existing `config.json`:

| File | Role | Writer | Consumer |
|------|------|--------|----------|
| `models.json` | Upstream registry snapshot | jenny (fetch on startup) | jenny (read-only) |
| `meta.json` | Dynamic runtime state — fetch metadata, exchange rates, future telemetry | jenny | jenny |
| `config.json` (existing) → `models` key | User-supplied field overrides on individual models | user (handwritten) | jenny (read-only) |

The two filesystem files are deliberately separate so their lifecycles do not collide: the fetch goroutine rewrites `models.json` and `meta.json`; the user rewrites `config.json`. A user editing their override block does not race with a fetch. Each file's presence or absence is independently valid.

**`models.json`** is the verbatim downloaded JSON from the upstream registry. No transformation on write; jenny reads it as read-only data. File size is ~5.5MB.

**`config.json`'s `models` key** is where the user expresses overrides. The shape mirrors the upstream `models` section: a top-level object keyed by provider id, each value a list of model objects. Each model object is a sparse patch — only the fields the user wants to override are present. Unspecified fields fall through to `models.json` (or bundled defaults if the snapshot is absent). Example:

```json
{
  "model": "claude-sonnet-4-6",
  "models": {
    "deepseek": [
      {
        "id": "deepseek-v4-flash",
        "maxOutput": 384000,
        "pricing": {
          "basePricing": { "textInput": 0.14, "textOutput": 0.28 },
          "currency": "USD"
        }
      }
    ]
  }
}
```

The example above overrides `maxOutput` and pricing for one model; everything else (abilities, modalities, contextWindow) falls through to the upstream snapshot. In practice, the upstream registry already carries these values correctly; user overrides are for cases where the upstream is stale or the operator has a private pricing tier. Override resolution precedence is **user config override > upstream snapshot > bundled defaults**. The `models` key in `config.json` is optional; its absence is the same as an empty override. Keeping the override in `config.json` rather than a separate file means there is one place for user-tunable settings, no second file to back up, and the override participates in the existing koanf precedence chain (CLI > env > config.json) for the rare case a model override is also flag-controlled.

**`meta.json`** holds the state jenny needs to decide whether to refresh and to expose runtime information that consumers may want to read without parsing the larger registry file:

```json
{
  "version": 1,
  "models": {
    "source": "https://raw.githubusercontent.com/ImSingee/aidy-models/master/models.json",
    "fetchedAt": "2026-06-19T00:00:00Z",
    "etag": "...",
    "schemaVersion": 1
  },
  "exchangeRates": {
    "updatedAt": "2026-06-19T00:00:00Z",
    "base": "USD",
    "rates": { "CNY": 7.25, "EUR": 0.92, "JPY": 155.0 }
  }
}
```

The `version` field at the root is the meta file's own schema version, distinct from the upstream's `schemaVersion`. Future fields (telemetry, API health checks, regional pricing tiers) add keys alongside `models` and `exchangeRates` without breaking older readers. The `meta.json` file is the right home for any "small, frequently-written, jenny-managed" state that should not pollute the 5.5MB snapshot.

The meta file is what jenny consults to decide whether the cache is fresh. The data files (`models.json` plus the `models` block in `config.json`) are what jenny reads for capability and pricing lookups.

### Fetch strategy

Jenny's startup already does I/O (load config, load session, check for updates). The fetch slots into this phase without adding a new blocking step:

1. **At startup, before the main loop**, jenny reads `meta.json` if it exists.
2. If the meta file is absent, **or `models.fetchedAt` is older than 24 hours**, **or the user explicitly requested a refresh** (e.g. `jenny --refresh-registry`), jenny triggers a fetch.
3. The fetch runs in a **goroutine spawned at the start of `NewClientWithModel` or equivalent**, and the result is awaited with a soft timeout (3 seconds). The main startup path does not block on it.
4. If the timeout fires, the goroutine continues in the background; jenny proceeds with whatever registry state is on disk (which may be stale, or absent). The goroutine writes its result to the cache only on success.
5. The fetch uses an HTTP client with a 10-second hard timeout, no retries (the upstream is a static file on GitHub's CDN; retrying on transient failure is acceptable only if the failure is detected within the 3-second startup window, which it won't be).
6. The fetch honors the `If-None-Match` ETag from the meta file. A 304 response is a successful no-op — meta file is not rewritten, data file is not re-downloaded.

The fetch path writes two artifacts: `models.json` (the downloaded bytes) and a refreshed `meta.json` (new `fetchedAt` and ETag). The user's `config.json` is never written by jenny — it is user-owned and jenny reads the `models` block from it via the existing koanf layer.

### Parallel handling

The fetch is one of several startup I/O operations. Each is independent and runs in its own goroutine:

- Load koanf config
- Load session metadata (if resuming)
- Load model registry (this proposal)
- Check for CLI updates (existing)

These four are gathered under a single `errgroup`-style fan-in. The main loop starts once all return, but with the **following key exception**: model registry is allowed to return `nil` (with a flag indicating "not loaded") without blocking startup. The other three are blocking. This asymmetry is by design — the registry is enhancement, not requirement.

### Override resolution

When a lookup is performed for a model `M`, the three-layer merge produces the effective record:

1. Start with the upstream record from `models.json[provider_id][i]` where `M.id == M`. If absent, skip to step 2.
2. Apply the user patch from `config.json`'s `models[provider_id][i]` block where `M.id == M`. The patch is a **shallow field merge at the model level, deep merge for the `pricing` sub-object**:
   - Top-level scalar fields (`maxOutput`, `contextWindow`, `releasedAt`, etc.) are replaced wholesale.
   - The `pricing` sub-object is merged field-by-field so the user can override just `currency` or just one of the `basePricing` entries without restating the whole pricing object.
   - Lists (`modalities.input`, `abilities.toolCall`, etc.) are replaced wholesale. There is no list-element merge — that ambiguity is too easy to get wrong, and a list is small enough to restate.
3. If neither source has the model, fall back to the bundled `internal/api/model_caps.go` table. If the bundled table also lacks the model, return `(_, false)` to the caller.

The patch is intentionally **non-recursive beyond the second level**. Users override a field or a small group of related fields, not arbitrary nested paths. This keeps the override block readable and the merge logic auditable in a single screen of code.

A `config.json` that fails to parse, or a `models` block within it that fails to parse, is not silently ignored — the parser emits a WARN log line and the override is treated as empty. The user can see the problem in their logs; the upstream registry is still consulted. A non-parseable override should never block the request.

A model block in `config.json` may also be supplied via a CLI flag or env var through the existing koanf layer if a user wants runtime override behavior; this is a future extension and not part of this proposal.

### Currency normalization

Model pricing arrives in the registry's native currency. From the verified upstream data: 89% of models have a `currency` field; currencies observed include `USD`, `CNY`, `EUR`, and a handful of others. A model priced in `CNY` is not directly comparable to a model priced in `USD` in cost reports.

`meta.json`'s `exchangeRates` block provides a single base currency (default `USD`) and a map of per-currency multipliers. Cost tracking consumes rates at the moment of billing, not at registry load time, so rate updates apply to subsequent usage without a restart. The rates object is sparse — currencies not present in the rates map are reported in their native currency with a `currency: "UNKNOWN"` warning in the cost log line. The rate fetch is a separate, optional concern from the model registry fetch; this proposal does not specify where the rates come from (a future enhancement). The rate consumer reads what is on disk; if rates are missing, cost tracking falls back to per-currency reporting without conversion.

### Graceful degradation

The registry is consumed by three subsystems today (capability resolution, cost tracking, future: protocol detection). Each subsystem shall continue to work when the registry is unavailable:

- **Capability resolution** (per `max-tokens-clamp`): resolution order is `config.json models override → models.json snapshot → bundled internal/api/model_caps.go table`. Each layer is optional; the next is consulted when the previous returns no record. The user can pin a specific capability for a model by adding it to their `config.json` without depending on either the snapshot or the bundled table being current.
- **Cost tracking**: falls back to **zero cost** for unknown models (current behavior). The registry, when present, populates per-model pricing. The transition is silent — no error, no warning at user level. A debug log line records "using bundled defaults for model X" when the registry is unavailable.
- **Protocol detection** (future use): falls back to the existing environment-variable-based provider selection. The registry is opportunistic.

The three subsystems never crash on a missing or malformed registry. The worst case is identical to today's behavior.

### Parsing

5.5MB JSON parse on a modern machine is ~50–100ms — within the 3-second soft timeout budget for the first fetch, but **not** acceptable as a synchronous read on every startup once cached. The cached file is parsed lazily: parsed on first lookup, then memoized in process memory. Parsing is `json.Decoder`-based, streaming, not `json.Unmarshal` of the whole file (the file is large enough that streaming matters for startup latency on cold cache).

A typed view `ModelRegistry` exposes lookups: `Capability(model string)`, `Pricing(model string)`, `ContextWindow(model string)`, `Modalities(model string)`, `Abilities(model string)`. Each returns `(value, ok)`; callers fall back to bundled defaults on `ok == false`.

### Refresh policy

| Trigger | Action |
|---|---|
| Cache missing | Fetch on next startup (background) |
| Cache older than 24h | Fetch on next startup (background) |
| User runs `jenny --refresh-registry` | Synchronous fetch, blocking, errors surface |
| User runs `jenny --offline` | Skip fetch entirely; use cache as-is |
| HTTP 304 | No-op |
| HTTP error / timeout / parse error | Log at WARN, keep existing cache, do not delete |

The cache is **never** deleted by jenny. A corrupt cache file is renamed to `model-registry.json.broken` and the missing-cache path runs as if no cache existed. The broken file is preserved for diagnosis.

### Schema versioning

The meta file's `schemaVersion` is a forward-compatible integer. Jenny refuses to read a registry with a higher `schemaVersion` than it understands — the cache is treated as missing in that case, and a fresh fetch is attempted. The `schemaVersion` of the aidy-models registry is currently implicit (defined by the file's structure); jenny records `1` as the version it understands. When the upstream structure changes incompatibly, jenny bumps its internal version and falls back to bundled defaults for registries with the older version.

## Rationale

**Why a background fetch with a soft timeout?**

The registry is enhancement, not requirement. Blocking startup for up to 3 seconds on a network call that may fail is a poor trade for data that has a working fallback. The user sees a slightly stale (or absent) registry for the first request, but startup remains fast. After the first successful background fetch, subsequent startups read the cache synchronously and are unaffected.

**Why bundled defaults alongside the registry?**

The registry is a moving target. The schema can change. The upstream can go down. Bundled defaults guarantee the system never regresses below today's behavior — and in the case of the capability table, "today's behavior" is now the comprehensive hand-maintained table from the `max-tokens-clamp` proposal. The registry is the *preferred* source, not the only source.

**Why 24 hours, not "always fetch"?**

Pricing drifts on a weekly-to-monthly cadence. Capability tables drift on a similar cadence. Fetching every minute burns bandwidth for no benefit; fetching every 24h captures drift at the cost of one cached read per day. The hard cap of 24h is configurable later if the data proves to drift faster, but 24h is the right starting point.

**Why ETag, not Last-Modified?**

ETag is exact; Last-Modified has second granularity and is invalidated by any re-pack. The aidy-models repo regenerates the file on every commit, so ETag is the only reliable freshness signal. If the upstream does not return ETag, jenny falls back to comparing the 24h age — the ETag path is opportunistic, not required.

**Why no retries on the fetch?**

The startup window is 3 seconds. Retries inside that window would compound the failure. A retry after 3 seconds, in the background, is fine — and that's what the goroutine does after returning `nil` to the startup fan-in. A separate background retry loop is out of scope; the next startup will try again.

**Why parse lazily, not at startup?**

A cached file is read on every startup. Parsing 5.5MB of JSON on every startup is ~50–100ms of pure CPU. Most startups do not need the registry — the user is running a one-shot query that doesn't touch pricing or capability resolution. Lazy parsing defers the cost to first lookup, after which the parsed result is memoized. The first lookup pays the parse cost; subsequent lookups are O(1).

**Why split user overrides into `config.json` instead of a dedicated file?**

The user override block is conceptually user configuration — values the user wants to set, in the same shape as other jenny user settings. Putting it in `config.json` keeps user-tunable state in one place, participates in the existing koanf precedence chain, and avoids a second file the user has to know about. A separate `models.override.json` would mean two user-edited files with no clear reason for the split, and would force the user to back up two files instead of one to preserve their setup across reinstalls. The only thing the override needs that other config doesn't is the sparse-patch merge semantics, which is a lookup-time concern, not a storage concern.

**Why two files (`models.json` + `meta.json`) on the jenny side, instead of one?**

The two files have different owners, different write cadences, and different failure modes. Conflating them creates problems that don't exist when they are separate:

- A user (or operator) looking at `meta.json` to understand "when was this fetched, what rates are in effect" gets an immediate answer without parsing 5.5MB. The size separation is also a clarity separation.
- Adding a future field like `telemetry` or `apiHealth` to `meta.json` does not require any change to the registry's read path, and vice versa.
- A corrupted `models.json` (e.g. truncated by a partial write) does not corrupt `meta.json`, and vice versa. Atomic-write protection is per-file.

The cost is two filesystem entries on the jenny side instead of one. That cost is trivial; the decoupling is not.

**Why is the override file sparse, not full-record?**

A full-record override forces the user to restate the entire model object on every change, which (a) duplicates upstream data the user doesn't care about, (b) makes the override file stop tracking upstream when the upstream changes the fields the user didn't override, and (c) makes merge logic fragile against upstream schema additions. A sparse patch file mirrors the upstream record by id; the merge fills in the rest. The user changes one field, one field changes; everything else continues to track upstream.

**Why deep-merge `pricing` but shallow-merge everything else?**

Pricing is a tightly coupled group of fields that the user almost always wants to override as a unit (changing one rate without changing the currency is incoherent). Other fields are independent. The asymmetry reflects the data, not the implementation.

**Why non-recursive merge beyond depth 2?**

Recursive deep merge at arbitrary depth is a well-known source of subtle bugs. Users override a field, not a path. A two-level merge is auditable in one screen of code; a recursive merge is a small library.

**Why read exchange rates at billing time, not at load time?**

Rate updates are decoupled from registry updates. A user (or a future automated process) can rewrite `meta.json` to refresh rates without touching the 5.5MB snapshot. Billing-time read means the change is visible on the next request, not on the next startup.



## Test Strategy

Unit tests:

- `ModelRegistry.Capability("claude-sonnet-4-6")` returns 64000 from a fixture
- `ModelRegistry.Capability("deepseek-v4-flash")` returns 384000 (validates against stale bundled default)
- Lookup of an unknown model returns `("", false)` — caller fallback path is exercised
- A malformed cache file is renamed to `.broken` and a fresh fetch is triggered
- ETag round-trip: 304 response does not rewrite the cache
- User override in `config.json` takes precedence over upstream snapshot
- Override resolution: a `config.json` `models` block patches a single field on one model; all other fields fall through to the upstream snapshot
- Override resolution: a `config.json` `models` block with no matching model id is silently ignored
- Override resolution: a `pricing` patch is merged field-by-field (overriding one rate leaves the others intact)
- Override resolution: a malformed `models` block in `config.json` produces a WARN log and is treated as empty

Integration tests:

- Cold start with no cache: fetch goroutine fires, first lookup blocks on it, fallback is used if the fetch is slow
- Warm start with fresh cache: no fetch attempted
- Warm start with stale cache: fetch goroutine fires, cache is used until the fetch completes
- `--offline` mode: no fetch attempted regardless of cache age

Manual verification:

- `jenny --refresh-registry` completes within 5s on a typical connection
- After a successful fetch, `~/.jenny/models.json` exists and parses
- Killing network mid-fetch does not corrupt the existing cache

## Out of Scope

- Periodic background refresh (e.g. a long-running daemon refreshing every 6h). The 24h-on-startup policy is sufficient; a periodic refresh is a separate optimization for long-running sessions.
- Mirroring the registry to a jenny-controlled CDN. The upstream is community-maintained; adding a mirror is a separate reliability project.
- Editing or extending the registry. Jenny is a consumer. Schema changes are upstream's responsibility; jenny reacts via `schemaVersion` bumping.
- A separate `models.dev/api.json` or `atzamis/ai-model-directory` integration. The aidy-models registry is a curated merge of `models.dev` and other sources; consuming it directly is unnecessary. The alternative registry is noted for future consideration if the primary becomes unmaintained.
- The `providers` section of the registry. This proposal only consumes the `models` section. The `providers` section (baseUrl, env, compat) is a future enhancement for protocol auto-detection and is not part of this change.

## Migration

This is a new subsystem. No migration of existing user data is required. The first run creates the cache; subsequent runs use it.

Order of work:

1. Add `internal/config/registry.go` with the fetch, parse, and lookup types.
2. Add `internal/config/registry_test.go` with the unit tests.
3. Wire the registry into startup (fan-in with config/session/CLI checks).
4. Add `jenny --refresh-registry` and `jenny --offline` flags.
5. Update `internal/api/model_caps.go` (from the `max-tokens-clamp` proposal) to consult the registry first, then the bundled table, then the user's config.json override block.
6. Update cost tracking to consult the registry, with zero-cost fallback.
7. Run full test suite; confirm cold-start and warm-start paths.
8. Once stable, fold the "External Model Registry" section into `docs/arch/provider-architecture.md` and delete this adhoc document.
