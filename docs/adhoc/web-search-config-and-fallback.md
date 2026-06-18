---
title: WebSearch config + client fallback (proposal)
slug: web-search-config-and-fallback
status: draft
date: 2026-06-18
updated: 2026-06-19
package: internal/tool
gaps:
  - isModelSupported is prefix-based and covers only claude-3/3.5/4; excludes OpenAI, Gemini, DeepSeek
  - No client-side search fallback (Tavily, SerpAPI, etc.)
  - No provider-aware capability check (SupportsNativeSearch)
  - No normalized search result type
depends_on:
  - tool-registry
  - provider-architecture
---
# WebSearch config + client fallback (proposal)

> **Status:** draft proposal — no code changes yet. Awaiting sign-off
> before promoting the spec parts into `docs/tools/web-search.md` and implementing.

## Problem

`internal/tool/web_search.go` has three concrete issues:

1. **`isModelSupported` is too generic** — name doesn't say *what* it gates.
   A future reader (or another tool in the package) will reasonably assume
   a different meaning.

2. **Model support is hardcoded by prefix** —
   `supportedWebSearchModels = ["claude-4", "claude-3.5", "claude-3"]`
   excludes OpenAI (GPT-5 series, o3, o4-mini) and Gemini (2.5 Pro/Flash with
   Search Grounding) even though both have native web search
   ([Claude](https://platform.claude.com/docs/en/agents-and-tools/tool-use/web-search-tool),
   [Gemini](https://ai.google.dev/gemini-api/docs/google-search),
   [OpenAI](https://platform.openai.com/docs/guides/tools-web-search)).

3. **No third-party client fallback** — providers like Tavily, SerpAPI,
   Exa, SearchApi.io can serve search results via a normal HTTP API.
   Today's code only knows about Anthropic's server-side tool.

## User's direction (this conversation, refined)

- Native provider's server-side search is the **default** when available.
- On unsupported model OR native failure, **fall back to a client provider**
  (Tavily first; SerpAPI / Exa / generic later).
- Configuration must go through the **existing koanf layering** — no
  new CLI flags and no new ad-hoc env vars. Operators set fields via
  `.jenny/config.json` and `JENNY_*` env vars per
  `docs/arch/koanf-config.md`.
- Client-provider results must be **normalized** to a single result shape.

## Key correction: native support is provider-bound, not model-bound

The first draft put a `native-models` pattern list at the top of the
config (e.g., `claude-4*`, `gpt-5*`, `gemini-2.*`). That is wrong:
the three providers' web search is implemented at the *provider* level,
not the *model* level, and the wire formats differ:

- **Anthropic**: `web_search_20250305` server tool, results arrive as
  `web_search_tool_result` content blocks.
- **OpenAI**: `web_search` tool, results are embedded in the message
  content (different schema, different citations).
- **Gemini**: Google Search grounding via `groundingMetadata`
  on the response; not a tool use at all.

A single tool schema cannot match all three. The right question is:
*"Is the active `ProviderKind` one that has web search capability?"* —
not *"Does this model name match a glob?"*.

`internal/api/provider.go:13` already defines `ProviderKind` with
`ProviderAnthropic` and `ProviderOpenAI`. Web search support is a
property of the kind, not the model.

### DeepSeek is not native

DeepSeek's API is wire-compatible with Anthropic's Messages API (per
[DeepSeek's Anthropic API guide](https://api-docs.deepseek.com/zh-cn/guides/anthropic_api)),
but web search is a *service* feature that Anthropic provides — DeepSeek
does not run search infrastructure. A DeepSeek-routed call that asks
for `web_search_20250305` will fail at the provider level, not at the
tool level. So DeepSeek routes go through client fallback, not native.

This matters for users who proxy Anthropic-format traffic through
DeepSeek-compatible endpoints expecting native search: it won't work,
and the fallback path is the correct one.

### Default native allowlist

The default set of provider kinds with native web search:

| `ProviderKind` | Native web search? | Notes |
|----------------|--------------------|-------|
| `anthropic` | yes | server tool `web_search_20250305` |
| `openai` | yes (model-gated: GPT-5/5.4/5.5 series, o3, o4-mini) | `web_search` tool |
| `openai_responses` | yes | same as `openai` |
| `gemini` | yes | Google Search grounding |

Operators can disable a default by overriding `native-providers` to an
explicit list, e.g., `["anthropic"]` to force Gemini/OpenAI to fall back.

For OpenAI, model-level gating (GPT-5 series vs GPT-4o/GPT-4.1) is a
*provider* concern, not a tool concern. The OpenAI provider knows which
models support `web_search` and either omits the tool from the request
or surfaces a clear error; the tool does not need to duplicate this
knowledge.

## Proposed design (revised)

### Top-level strategy selector

A new top-level setting `web-search.provider` chooses the execution mode:

| Value | Behavior |
|-------|----------|
| `native` (default) | Use native server-side when the active provider reports `SupportsNativeSearch() == true`; fall back to configured client otherwise or on native failure. |
| `client` | Always use the configured client provider. |
| `disabled` | Tool unavailable; never registered. |

Resolved via the standard koanf layering — the field is read through
the `Flags` struct (`koanf:"web-search-provider"`) and surfaces as
both JSON config (`web-search.provider`) and `JENNY_WEB_SEARCH_PROVIDER`.
**No new CLI flag**, **no new env var convention** beyond what koanf
already produces.

### Native capability

Native support is reported by the active provider via
`provider.SupportsNativeSearch() bool`. The tool queries the provider
at runtime; it does not carry a model-name allowlist.

The default providers that return `true`:

| `ProviderKind` | Native web search? | Notes |
|----------------|--------------------|-------|
| `anthropic` | yes | server tool `web_search_20250305` |
| `openai` | yes (model-gated: GPT-5/5.4/5.5 series, o3, o4-mini) | `web_search` tool |
| `openai_responses` | yes | same as `openai` |
| `genai` | yes | Google Search grounding |

Operators can override the global default by setting
`web-search.force-client: true` in JSON config (or
`JENNY_WEB_SEARCH_FORCE_CLIENT=true`), which makes the tool always
fall back to the client provider regardless of provider capability.
This is useful for cost control or when the native provider's quota
is exhausted.

### Client-provider config

```jsonc
{
  "web-search": {
    "client": {
      "provider": "tavily",   // or "serp", "custom", ...
      "api-key": "env:TAVILY_API_KEY",
      "base-url": "",          // required when provider=="custom"
      "max-results": 8
    }
  }
}
```

`api-key` accepts the same three forms the router already uses for
`Account.Key` (`env:NAME`, `literal:VALUE`, plain literal — see
`internal/api/router/config.go:69`). That keeps the resolution logic
in one place rather than inventing a parallel convention.

Provider implementations:

- `tavily` — `POST https://api.tavily.com/search`, Bearer token auth
  (`Authorization: Bearer <api-key>`). Credit-based pricing:
  free tier 1000 credits/month; basic search 1 credit, advanced 2 credits;
  pay-as-you-go $0.008/credit, monthly plans from $0.005–$0.0075/credit.
  Docs: <https://docs.tavily.com>.
- `custom` — generic HTTP endpoint with a documented JSON request/response
  shape; used as a placeholder until dedicated providers (SerpAPI, Exa,
  SearchApi.io) land. Operators can wire it for any HTTP search API
  without code changes.

### Normalized result type

A single struct replaces the current ad-hoc "API handles it server-side"
path with an explicit result shape:

```go
type SearchResult struct {
    Title          string  `json:"title"`
    URL            string  `json:"url"`
    Snippet        string  `json:"snippet"`
    Source         string  `json:"source"`          // "tavily" | "anthropic" | ...
    RelevanceScore float64 `json:"relevance_score"`  // [0, 1]; 0 means unset
}

type SearchResponse struct {
    Query     string         `json:"query"`
    Results   []SearchResult `json:"results"`
    Provider  string         `json:"provider"`   // who produced this response
    LatencyMs int64          `json:"latency_ms"`
}
```

Native execution fills this from `web_search_tool_result` blocks (Anthropic),
the OpenAI `web_search` tool output, or `groundingMetadata` (Gemini) —
each provider in `internal/api/` normalizes its own response into this
shape, then the engine hands it back to the WebSearchTool.

Client execution fills it from the HTTP API's response.
The tool renders both into the same tool result text so the agent
sees no difference.

### Tool wiring (pseudo-flow)

```
WebSearchTool.Execute(query, opts):
    validate query length, mutual exclusion              # existing AC1, AC3
    check call counter                                  # existing cap
    select strategy by WebSearchConfig.Provider:
        "disabled" → error "web search disabled"
        "native":
            if activeProvider.SupportsNativeSearch()
               && !WebSearchConfig.ForceClient:
                result = nativeRunner.RunNativeSearch(...)
                if result == nil || result.IsError:
                    result = client.Search(...)         # fallback once
            else:
                result = client.Search(...)
        "client":
            result = client.Search(...)
    render SearchResponse into tool result content
```

The tool keeps `Name() = "web_search"`, its `InputSchema`, and the
existing call counter. Only the body of `Execute` and the
constructor's inputs change.

### SearchClientProvider interface

```go
type SearchClientProvider interface {
    Name() string
    Search(ctx context.Context, query string, opts SearchOptions) (*SearchResponse, error)
}
```

Each provider stamps its own `Provider` field on the response.
A thin `NormalizedClientSearchProvider` wrapper handles the
`LatencyMs` measurement and ensures the response shape is uniform.

### Native execution as an engine hook

Native search is *not* a client provider — the model does it
server-side and returns results in provider-specific blocks. The tool
needs an engine-level hook to retrieve those blocks:

```go
type NativeSearchRunner interface {
    RunNativeSearch(ctx context.Context, query string,
                    allowedDomains, blockedDomains []string) (*NativeSearchResult, error)
}
```

The engine implements this once per `ProviderKind`; the tool takes it
as a constructor arg alongside the `WebSearchConfig` and the
`SearchClientProvider` instance.

## File / package layout

New files (all in `internal/tool/`):

| File | Purpose |
|------|---------|
| `search_config.go` | `WebSearchConfig`, `ClientConfig`, `ResolveWebSearchConfig`, `ResolveClientAPIKey`, defaults. |
| `search_provider.go` | `SearchResult`, `SearchResponse`, `SearchClientProvider` interface, `NativeSearchRunner` interface. |
| `search_tavily.go` | Tavily implementation. |
| `search_custom.go` | Generic HTTP implementation for `provider: "custom"`. |
| `search_render.go` | `SearchResponse` → tool-result-text rendering (native and client render identically). |

Existing `internal/tool/web_search.go` refactor:

- Replace `isModelSupported` with a check against `provider.SupportsNativeSearch()`
- Accept a `*WebSearchConfig`, a `NativeSearchRunner`, and a `SearchClientProvider`
- Implement the strategy-selection pseudo-flow above
- Remove the `WithModel(model)` constructor arg; the tool takes the
  active `Provider` instead (so it can query `SupportsNativeSearch()`)

`internal/api/provider_anthropic.go`, `provider_openai.go`,
`provider_genai.go` each add a `RunNativeSearch` implementation that
extracts their provider-specific search results and normalizes them
into `*SearchResponse`.

### Tests

Existing `internal/tool/web_search_test.go` cases (AC1, AC3, AC5)
keep working with minor signature updates. New tests:

- `TestProvider_SupportsNativeSearch` — each kind reports correctly
- `TestResolveWebSearchConfig` — JSON, env, default precedence
- `TestResolveClientAPIKey` — `env:NAME`, `literal:X`, plain literal
- `TestTavilyProvider` — httptest happy path, 4xx, 5xx, timeout
- `TestCustomProvider` — httptest with documented JSON shape
- `TestSearchResponseRender` — native vs client rendering parity
- `TestWebSearchTool_Fallback` — native-ok, native-fail, native-unsupported, force-client

## Spec doc updates

Promote this design into `docs/tools/web-search.md`:

- Replace the **Provider Gating** section with strategy-selector +
  native-providers description
- Add **Client Provider Configuration** section
- Add **Execution Strategy** section (native-first, fallback)
- Add **Normalized Result Type** section
- Extend AC4 to reference provider-kind allowlist
- Add AC5-AC11 (fallback, normalize, Tavily, env keys, precedence)

## Out of scope (explicit non-goals)

- **No CLI flag, no new env convention** — koanf handles both via
  `koanf:"web-search-provider"` etc. on the `Flags` struct.
- **No new dependencies** — Tavily is a plain `net/http` POST.
- **No streaming search results** — every provider returns the full
  response.
- **No caching layer in v1** — Tavily already caches intelligently;
  a Jenny-level cache can land later.
- **No SerpAPI / Exa / SearchApi.io in v1** — schema is in place; adding
  named providers is incremental work.

## Open questions for the user — resolved

1. **Provider kind list source of truth** — `internal/api`. The
   `Provider` interface gets `SupportsNativeSearch() bool`; the tool
   queries the active provider instead of carrying its own allowlist.
   This way adding a new provider with native search doesn't require
   touching the tool.

2. **`ProviderGenAI` constant** — yes, added. Pre-existing gap that web
   search surfaces; land it in the same change.

3. **Fallback trigger** — any non-2xx from the engine hook falls back to
   the client provider (once). Transient retry stays inside the
   engine's existing retry layer.

4. **`custom` provider in v1** — yes, shipped alongside `tavily`. The
   generic HTTP plumbing covers most of what's needed and lets operators
   wire any HTTP search API without a code change.

## Roll-out

1. Land spec update in `docs/tools/web-search.md` (per project rules).
2. Land tests first per existing convention.
3. Land code in this order:
   - Add `ProviderGenAI` constant + `SupportsNativeSearch()` on `Provider`
   - Config types in `internal/tool/`
   - `SearchClientProvider` interface + Tavily + custom
   - Native hook implementations in each provider
   - `WebSearchTool` refactor
4. Deprecate `isModelSupported` and the `supportedWebSearchModels`
   constant; remove on next minor.