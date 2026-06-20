// Package api provides centralized model capability resolution and max_tokens
// clamping for all API providers.
package api

import (
	"strings"

	"github.com/ipy/jenny/internal/config"
	"github.com/ipy/jenny/internal/log"
)

// unknownModelMaxTokens is the conservative fallback when a model does not
// match any entry in the capability table.
const unknownModelMaxTokens = 16384

// modelCapEntry holds a model name prefix/glob and its capabilities.
type modelCapEntry struct {
	pattern              string
	contextWindow        int
	maxOut               int
	hasVision            bool // whether the model supports image/vision input
	supportsNativeSearch bool // whether the model supports native web search
}

// defaultContextWindow is the fallback context window for unknown models.
const defaultContextWindow = 200_000

// modelCapTable maps model name patterns to their capabilities.
// Patterns are matched as case-insensitive prefixes. More specific patterns
// must appear before less specific ones (table is evaluated in order, first
// match wins).
//
// Source: https://github.com/ImSingee/aidy-models (2026-06-21 snapshot)
var modelCapTable = []modelCapEntry{
	// Anthropic
	{"claude-fable-5", 1000000, 128000, true, true},
	{"claude-opus-4-6", 1000000, 128000, true, true},
	{"claude-opus-4-7", 1000000, 128000, true, true},
	{"claude-opus-4-8", 1000000, 128000, true, true},
	{"claude-sonnet-4-6", 1000000, 64000, true, true},
	// Catch-all for older Claude models
	{"claude-opus-4-", 200000, 128000, true, true},
	{"claude-sonnet-4-", 200000, 64000, true, true},
	{"claude-haiku-4-", 200000, 64000, true, true},

	// OpenAI
	{"gpt-5.5-pro", 1050000, 128000, true, true},
	{"gpt-5.5", 1050000, 128000, true, true},
	{"gpt-5.4-pro", 1050000, 128000, true, true},
	{"gpt-5.4", 1050000, 128000, true, true},
	{"gpt-5.4-mini", 400000, 128000, true, true},
	{"gpt-5.4-nano", 400000, 128000, true, true},
	{"gpt-5.3-codex", 400000, 128000, true, true},
	{"gpt-image-2", 0, 0, true, false},
	// Catch-all for older GPT-5 variants
	{"gpt-5", 200000, 128000, true, true},
	{"gpt-4.1", 200000, 33000, true, true},
	{"gpt-4o", 200000, 16384, true, true},
	{"o3", 200000, 100000, false, true},
	{"o4-mini", 200000, 100000, false, true},

	// DeepSeek
	{"deepseek-v4-pro", 1048576, 393216, false, false},
	{"deepseek-v4-flash", 1048576, 393216, false, false},
	{"deepseek-chat", 1000000, 384000, false, false},
	// Catch-all for other DeepSeek model names
	{"deepseek", 1000000, 384000, false, false},

	// Google
	{"gemini-3.5-flash", 1114112, 65536, true, true},
	{"gemini-3.1-pro-preview-customtools", 1048576, 65536, true, false},
	{"gemini-3.1-pro-preview", 1114112, 65536, true, true},
	{"gemini-3.1-flash-lite-preview", 1048576, 65536, true, true},
	{"gemini-3.1-flash-lite", 1114112, 65536, true, true},
	{"gemini-3.1-flash-image-preview", 163840, 32768, true, true},
	{"gemma-4-31b-it", 294912, 32768, true, true},
	{"gemma-4-26b-a4b-it", 294912, 32768, true, true},
	// Catch-all for older Gemini models
	{"gemini-2.5-", 200000, 65536, true, true},
	{"gemini-2.0-", 200000, 65536, true, true},

	// Alibaba
	{"qwen3.7-max", 1000000, 65536, false, false},
	{"qwen3.7-plus", 1000000, 65536, true, false},
	{"qwen3.6-plus", 1000000, 65536, true, false},
	{"qwen3.6-flash", 1000000, 65536, true, false},
	{"qwen3.6-35b-a3b", 262144, 65536, true, false},
	{"qwen3.6-27b", 262144, 65536, true, false},
	{"qwen3.5-397b-a17b", 262144, 65536, true, false},
	{"qwen3.5-122b-a10b", 262144, 65536, true, false},

	// Moonshot / Kimi
	{"kimi-k2.7-code", 262144, 262144, true, false},
	{"kimi-k2.6", 262144, 32768, true, false},
	{"kimi-k2.5", 262144, 32768, true, false},

	// MiniMax
	{"MiniMax-M3", 1000000, 524288, true, false},
	{"MiniMax-M2.7-highspeed", 204800, 131072, false, false},
	{"MiniMax-M2.7", 204800, 131072, false, false},
	{"MiniMax-M2.5-highspeed", 204800, 131072, false, false},
	{"MiniMax-M2.5", 204800, 131072, false, false},

	// ZhipuAI
	{"glm-5.2", 1000000, 131072, false, false},
	{"glm-5.1", 200000, 131072, false, true},
	{"glm-5v-turbo", 200000, 131072, true, true},
	{"glm-5-turbo", 200000, 131072, false, true},
	{"glm-5", 200000, 131072, false, true},
	{"glm-4.7-flash", 200000, 131072, false, true},

	// Xiaomi
	{"mimo-v2.5-pro", 1000000, 131072, false, true},
	{"mimo-v2.5", 1000000, 131072, true, true},
	{"mimo-v2-pro", 1048576, 131072, false, false},
	{"mimo-v2-omni", 262144, 131072, true, false},
	{"mimo-v2-flash", 262144, 65536, false, true},

	// xAI
	{"grok-4.3", 1000000, 30000, true, true},
	{"grok-build-0.1", 256000, 256000, true, false},

	// StepFun
	{"step-3.5-flash", 256000, 256000, false, true},
}

// ResolveMaxTokens returns the resolved max_tokens value for a given model and
// caller-supplied override.
//
// Rules:
//   - If override > 0 and <= model capability, return override unchanged.
//   - If override > 0 and > model capability, clamp to capability and emit a
//     structured WARN log (but do not return an error — the call proceeds with
//     the clamped value).
//   - If override <= 0, return the model's full capability (default).
//   - For unknown models, return the conservative fallback (16384) and emit a
//     WARN log with reason "unknown_model_capability_default".
func ResolveMaxTokens(model string, override int) int {
	cap := lookupModelCap(model)

	if override <= 0 {
		if override < 0 {
			log.Warn("max_tokens: negative override clamped",
				"model", model,
				"override", override,
				"resolved", cap,
				"reason", "negative_override",
			)
		}
		return cap
	}

	if override <= cap {
		return override
	}

	log.Warn("max_tokens: override exceeds model capability, clamping",
		"model", model,
		"override", override,
		"capability", cap,
		"resolved", cap,
		"reason", "override_exceeds_capability",
	)
	return cap
}

// normalizeModelName strips provider prefixes from model identifiers such as
// workers-ai/@cf/meta/llama-3.1-8b-instruct-fp8 and deepseek-anthropic/deepseek-v4-pro,
// returning the bare model name.
func normalizeModelName(model string) string {
	// Split on the last '/' — providers may use multi-level IDs like
	// workers-ai/@cf/meta/llama-3.1-8b-instruct-fp8.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// lookupModelCap finds the max output tokens for a model by consulting the
// external model registry first, then falling back to the bundled capability table.
// For model names with provider prefixes (e.g. deepseek/deepseek-v4-pro), it
// first tries the original name, then retries with the normalized bare name.
// Returns unknownModelMaxTokens for unrecognized models.
func lookupModelCap(model string) int {
	reg := config.GlobalRegistry()

	// Consult external model registry first (original name)
	if reg != nil {
		if cap, ok := reg.Capability(model); ok {
			log.Debug("max_tokens: using registry capability",
				"model", model,
				"capability", cap,
			)
			return cap
		}
	}

	bare := normalizeModelName(model)

	// Retry registry with bare name (if different from original)
	if bare != model && reg != nil {
		if cap, ok := reg.Capability(bare); ok {
			log.Debug("max_tokens: using registry capability (normalized)",
				"model", model,
				"normalized", bare,
				"capability", cap,
			)
			return cap
		}
	}

	// Try bundled capability table with original name, then bare name
	lower := strings.ToLower(model)
	for _, e := range modelCapTable {
		if strings.HasPrefix(lower, strings.ToLower(e.pattern)) {
			return e.maxOut
		}
	}

	if bare != model {
		lowerBare := strings.ToLower(bare)
		for _, e := range modelCapTable {
			if strings.HasPrefix(lowerBare, strings.ToLower(e.pattern)) {
				return e.maxOut
			}
		}
	}

	log.Warn("max_tokens: unknown model, using conservative default",
		"model", model,
		"default_max_output_tokens", unknownModelMaxTokens,
		"reason", "unknown_model_capability_default",
	)
	return unknownModelMaxTokens
}

// SupportsVision reports whether the given model supports image/vision input.
// It consults the external registry first, then falls back to the bundled
// capability table. Unknown models are assumed to support vision (conservative
// for multimodal APIs — avoids false refusals for unfamiliar but capable models).
func SupportsVision(model string) bool {
	reg := config.GlobalRegistry()

	// Consult external model registry first
	if reg != nil {
		if modalities, ok := reg.Modalities(model); ok {
			for _, m := range modalities {
				if m == "image" || m == "vision" {
					return true
				}
			}
			return false
		}
	}

	bare := normalizeModelName(model)

	// Retry registry with bare name (if different)
	if bare != model && reg != nil {
		if modalities, ok := reg.Modalities(bare); ok {
			for _, m := range modalities {
				if m == "image" || m == "vision" {
					return true
				}
			}
			return false
		}
	}

	// Fall back to bundled table: unknown models default to true
	lower := strings.ToLower(model)
	for _, e := range modelCapTable {
		if strings.HasPrefix(lower, strings.ToLower(e.pattern)) {
			return e.hasVision
		}
	}
	if bare != model {
		lowerBare := strings.ToLower(bare)
		for _, e := range modelCapTable {
			if strings.HasPrefix(lowerBare, strings.ToLower(e.pattern)) {
				return e.hasVision
			}
		}
	}
	return true // conservative default
}

// modelMaxOutputCap returns the max output token capability for a model
// by delegating to the canonical lookupModelCap.
func modelMaxOutputCap(model string) int {
	return lookupModelCap(model)
}

// modelContextWindowLookup returns the context window size for a given model.
// Consults the external model registry first, then falls back to the bundled
// capability table. Returns defaultContextWindow for unrecognized models.
func modelContextWindowLookup(model string) int {
	reg := config.GlobalRegistry()
	if reg != nil {
		if cw, ok := reg.ContextWindow(model); ok {
			return cw
		}
	}

	bare := normalizeModelName(model)

	// Retry registry with bare name (if different from original)
	if bare != model && reg != nil {
		if cw, ok := reg.ContextWindow(bare); ok {
			return cw
		}
	}

	lowerBare := strings.ToLower(bare)
	for _, e := range modelCapTable {
		if strings.HasPrefix(lowerBare, strings.ToLower(e.pattern)) {
			return e.contextWindow
		}
	}

	return defaultContextWindow
}

// SupportsNativeSearch reports whether the given model supports native web search.
// Unknown models default to true (conservative — most providers support it).
func SupportsNativeSearch(model string) bool {
	lower := strings.ToLower(model)
	for _, e := range modelCapTable {
		if strings.HasPrefix(lower, strings.ToLower(e.pattern)) {
			return e.supportsNativeSearch
		}
	}
	bare := normalizeModelName(model)
	if bare != model {
		lowerBare := strings.ToLower(bare)
		for _, e := range modelCapTable {
			if strings.HasPrefix(lowerBare, strings.ToLower(e.pattern)) {
				return e.supportsNativeSearch
			}
		}
	}
	return true // conservative default
}
