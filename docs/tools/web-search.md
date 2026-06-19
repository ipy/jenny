---
title: WebSearch Tool
slug: web-search
priority: P3
status: done
spec: partial
code: done
package: internal/tool
gaps:
  - allowed_domains/blocked_domains accepted in schema but not wired to provider interfaces
  - NativeSearchRunner interface exists but no production implementation is wired
depends_on:
  - tool-registry
---
# WebSearch Tool

## Overview

Web search via native provider server-side search or client-provider fallback. The tool selects the execution path based on a configurable strategy and the active provider's native search capability.

## Parameters

| Param | Description |
|-------|-------------|
| `query` | Search query (min length 2) |
| `allowed_domains` | Restrict results (mutually exclusive with blocked) — accepted but not yet wired to providers |
| `blocked_domains` | Exclude domains — accepted but not yet wired to providers |

## Limits

- Max **8** server searches per agent session.
- Query min length **2**.
- Max **8** results per search.

## Strategy Selection

The tool's behavior is governed by a `WebSearchConfig` resolved through koanf layering (`.jenny/config.json` + `JENNY_*` env vars). The top-level strategy selector `web-search.provider` chooses the execution mode:

| Value | Behavior |
|-------|----------|
| `native` (default) | Use native server-side search when the active provider reports `SupportsNativeSearch() == true`. Fall back to the configured client provider if native is unsupported or fails. |
| `client` | Always use the configured client provider. |
| `disabled` | Tool unavailable; returns an error before any network call. |

### Config resolution precedence

Env vars override JSON config overrides defaults. The resolved config fields:

| Config key | Env var | Description |
|------------|---------|-------------|
| `web-search.provider` | `JENNY_WEB_SEARCH_PROVIDER` | Strategy: `native`, `client`, or `disabled` |
| `web-search.client.provider` | `JENNY_WEB_SEARCH_CLIENT_PROVIDER` | Client provider: `tavily` or `custom` |
| `web-search.client.api-key` | `JENNY_WEB_SEARCH_CLIENT_API_KEY` | API key in `env:NAME`, `literal:VALUE`, or plain form |
| `web-search.client.base-url` | `JENNY_WEB_SEARCH_CLIENT_BASE_URL` | Required for `custom` provider |

## Provider Gating

The tool queries the active provider's `SupportsNativeSearch() bool` method instead of maintaining a model-name allowlist. Native web search support is a provider-level property:

| `ProviderKind` | Native web search? | Notes |
|----------------|--------------------|-------|
| `anthropic` | yes | Server tool `web_search_20250305` |
| `openai` | yes | All models except DeepSeek |
| `openai_responses` | yes | All models unconditionally |
| `genai` | yes | Google Search grounding |
| Other (e.g. DeepSeek) | no | Falls back to client provider |

## Client Providers

When native search is unavailable (provider doesn't support it, or native execution fails), the tool falls back to a client-side search provider.

### Tavily

Makes a `POST` to `https://api.tavily.com/search` with Bearer token authentication. Credit-based pricing: free tier 1000 credits/month.

### Custom

Generic HTTP endpoint for any search API. Configure `base-url` and `api-key` fields. The provider sends a documented JSON request shape and normalizes the response.

## Normalized Results

Both native and client search paths produce a `SearchResponse` struct rendered identically into tool result text:

```go
type SearchResult struct {
    Title   string `json:"title"`
    URL     string `json:"url"`
    Snippet string `json:"snippet"`
}

type SearchResponse struct {
    Query   string         `json:"query"`
    Results []SearchResult `json:"results"`
}
```

Native search execution fills this from provider-specific result blocks (e.g., Anthropic `web_search_tool_result`). Client execution fills it from the HTTP API response. The tool renders both paths through a shared renderer, so the agent sees identical-format output.

## Execution Flow

```
WebSearchTool.Execute(query, opts):
    validate query length, mutual exclusion
    check call counter (max 8 per session)
    select strategy:
        "disabled" -> error "web search disabled"
        "client" -> executeClientSearch -> render
        "native":
            if provider.SupportsNativeSearch() and nativeRunner exists:
                result = nativeRunner.RunNativeSearch(...)
                if result != nil and no error:
                    render result
                    return
            fallbackToClient(query) -> render
```

## Native Search Runner

Native search is executed through the `NativeSearchRunner` interface, which each provider implements to extract and normalize its provider-specific search results. The runner is an engine-level hook provided to the tool at construction time.

## Errors

- Unsupported query length returns a clear error message.
- Mutual exclusion violation returns an error message.
- Disabled strategy returns "web search disabled".
- Unavailable client provider returns a descriptive error message.
- Native search failures fall back to the client provider exactly once; if the client also fails, the error is surfaced.

## Acceptance Criteria

- **AC1:** Query length >= 2.
- **AC2:** Max 8 searches per agent session.
- **AC3:** allowed_domains XOR blocked_domains.
- **AC4:** Unsupported provider (no native + no client) returns clear error.
- **AC5:** API/network errors return actionable tool error messages.
- **AC6:** Strategy selection follows config precedence (env > JSON > default).
- **AC7:** `env:` prefix in api-key resolves from environment variable.
- **AC8:** Tavily provider makes correct HTTP request and normalizes response.
- **AC9:** Custom provider makes correct HTTP request and normalizes response.
- **AC10:** Native and client results render identically.
- **AC11:** Native failure falls back to client exactly once; double failure surfaces client error.
