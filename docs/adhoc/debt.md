---
title: Technical Debt
slug: debt
status: open
date: 2026-06-19
updated: 2026-06-19 (mD-7)
---

# Technical Debt

Outstanding debt from devloop review cycles. Ordered by severity then task.

## CRITICAL

### ~~CD-1: `--refresh-registry` skips user override application~~
- ~~**Task:** external-model-registry~~
- ~~**File:** `cmd/jenny/main.go:191-196`~~
- ~~The `--refresh-registry` block calls `reg.Fetch()` and returns on success before the user override loading block runs. `jenny --refresh-registry` never applies user overrides from config.json.~~
- ~~**Fix:** Move user override loading before the `--refresh-registry` block, or re-apply overrides after `Fetch()`.~~

### ~~CD-2: `--refresh-registry` and `--offline` not mutually exclusive~~
- ~~**Task:** external-model-registry~~
- ~~**File:** `cmd/jenny/main.go:185-196`~~
- ~~`Fetch()` does not check `r.offline`. Passing both flags causes a network request despite offline mode.~~
- ~~**Fix:** Make `Fetch()` check `r.offline` and return an error, or make flags mutually exclusive in `cli.go`.~~

## MAJOR

### ~~MD-1: Fix inaccurate test comments (wrong constant value)~~
- ~~**Task:** max-tokens-clamp~~
- ~~**Files:** `internal/agent/engine_test.go:2396`, `internal/agent/compact_test.go:509`~~
- ~~Comments reference non-existent `50000` instead of `13000` (`minAutoCompactBuffer`).~~

### ~~MD-2: Missing integration test for streaming-fallback max_tokens~~
- ~~**Task:** max-tokens-clamp~~
- ~~The spec requires: "The streaming fallback path produces the same clamped value as the primary streaming path." No test verifies this for the actual `SendMessageStream` → fallback path.~~

### ~~MD-3: `MaxOutputTokens` is 0 for `CategoryContextExhausted`~~
- ~~**Task:** max-tokens-clamp~~
- ~~**File:** `internal/api/provider_anthropic.go:671-677`~~
- ~~When `contextRejected` is true, `MaxOutputTokens` is left at zero value. Spec says this field should be trustworthy for all categories.~~

### ~~MD-4: `openAIProvider.SupportsNativeSearch()` uses exclusion logic~~
- ~~**Task:** web-search-config-and-fallback~~
- ~~**File:** `internal/api/provider_openai.go:98-99`~~
- ~~Returns `!isDSModel(p.model)` — too broad. Non-OpenAI models routed through the OpenAI-compatible provider incorrectly report native search support.~~
- ~~**Fix:** Use explicit allowlist of OpenAI model families (e.g., `gpt-`, `o3`, `o4`, `chatgpt-`).~~

### MD-5: Zero test coverage for `SupportsNativeSearch()` in `internal/api/`
- **Task:** web-search-config-and-fallback
- No `*_test.go` files in `internal/api/` test this method. The only test (`TestProvider_SupportsNativeSearch` in `search_test.go`) checks a nil interface.

### MD-6: API key leaked in error response bodies
- **Task:** web-search-config-and-fallback
- **Files:** `internal/tool/search_tavily.go:73`, `internal/tool/search_custom.go:68`
- Raw error response bodies included in error strings — credential-leak vector if remote server echoes the API key.
- **Fix:** Truncate error bodies to safe length (e.g., 200 chars) or redact API key.

### MD-7: Duplicate `ModelPricing` type in two packages
- **Task:** external-model-registry
- **Files:** `internal/config/registry.go:64`, `internal/agent/cost.go:17`
- Identical structs in separate packages with manual field copying. Consolidate into `config.ModelPricing` as single source of truth.

### MD-8: `modelMaxOutputCap` duplicates `lookupModelCap` logic
- **Task:** external-model-registry (same as max-tokens-clamp debt #5)
- **File:** `internal/api/model_caps.go`
- Both functions have identical registry-then-bundled-table lookup. Delegate one to the other.

### MD-9: Background fetch goroutine has no cancellation
- **Task:** external-model-registry
- **File:** `cmd/jenny/main.go:199-214`
- After 3s soft timeout, inner goroutine continues with 30s HTTP timeout. Use a context with deadline.

### MD-10: Config.json `models` key read via `os.ReadFile`, bypassing koanf
- **Task:** external-model-registry
- **File:** `cmd/jenny/main.go:224-225`
- Config file read a second time; should extract `models` key from existing koanf instance.

## MINOR

### mD-1: Remove dead `setMaxTokensOverride` code
- **Task:** max-tokens-clamp
- **Files:** `internal/api/client.go`, all four provider files
- `Client.setMaxTokensOverride`, `Client.maxTokensOverride` field, and four provider methods are dead code.

### mD-2: `ValidateWebSearchConfig` only validates client config for `StrategyClient`
- **Task:** web-search-config-and-fallback
- **File:** `internal/tool/search_config.go:123-130`
- Should validate `ClientConfig` whenever `ClientConfig.Provider` is set, regardless of strategy.

### mD-3: `buildPrintTools` creates WebSearchTool without wiring
- **Task:** web-search-config-and-fallback
- **File:** `cmd/jenny/main.go:543-553`
- No config, native runner, or client provider wired. Latent correctness issue if `Execute()` is ever called.

### mD-4: `PricingOverride` JSON tags use snake_case for cache fields
- **Task:** external-model-registry
- **File:** `internal/config/registry.go:73-78`
- Upstream registry uses `cacheRead`/`cacheCreation` (camelCase). Config.json overrides use `cache_read`/`cache_creation` (snake_case). Align conventions.

### mD-5: `ParseConfigModels` returns `nil, nil` on parse error
- **Task:** external-model-registry
- **File:** `internal/config/registry.go:362-363`
- Misleading function signature — returns error type but never populates it.

### mD-6: Missing integration tests for registry startup paths
- **Task:** external-model-registry
- Spec calls for cold start, warm start with fresh cache, warm start with stale cache, and `--offline` integration tests.

### mD-7: Test suite uses Unix-only `/tmp` paths, not cross-platform
- **Files:** `internal/agent/loop_test.go`, `internal/agent/executor_test.go`, `internal/agent/subagent_test.go`, `internal/agent/prompt_active_skills_test.go`, `internal/tool/bash_test.go`, `internal/tool/task_*.go`, `internal/mcp/client_test.go`, `internal/mcp/list_resources_test.go`, `internal/portal/portal_test.go`, `internal/grepinproc/adapter_test.go`, `internal/tool/lsp_test.go`, `internal/tool/structured_output_test.go`, `internal/tool/exit_worktree_test.go`, `internal/tool/read_mcp_resource_test.go` (~20 files, ~50+ occurrences)
- Tests pass `"/tmp"` as `cwd` to tool executors and `buildSystemPrompt`. On Windows, `/tmp` doesn't exist as a valid path — `git.GetRoot("/tmp")` silently fails (returns `ok=false`) and `contextSection("/tmp")` produces a wrong `"Cwd: /tmp"` string. Tests don't assert on these values so they pass despite producing semantically wrong behavior.
- **Fix:** Replace all `"/tmp"` cwd arguments in test files with `t.TempDir()`. Mechanical replacement; no behavioral changes to assertions. `t.TempDir()` is cross-platform and auto-cleaned.
- **Task:** external-model-registry
- Spec calls for cold start, warm start with fresh cache, warm start with stale cache, and `--offline` integration tests.
