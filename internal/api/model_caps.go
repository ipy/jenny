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

// modelCapEntry holds a model name prefix/glob and its max output tokens.
type modelCapEntry struct {
	pattern   string
	maxOut    int
	hasVision bool // whether the model supports image/vision input
}

// modelCapTable maps model name prefixes to their maximum output token limits.
// hasVision indicates whether the model accepts image input. Unknown models
// are assumed to support vision (conservative for multimodal APIs).
// Patterns are matched as case-insensitive prefixes. More specific patterns
// must appear before less specific ones (table is evaluated in order, first
// match wins).
var modelCapTable = []modelCapEntry{
	// Claude models
	{"claude-opus-4-", 128000, true},
	{"claude-fable-5", 128000, true},
	{"claude-sonnet-4-", 64000, true},
	{"claude-haiku-4-", 64000, true},

	// GPT models
	{"gpt-5", 128000, true},
	{"gpt-4.1", 33000, true},
	{"gpt-4o", 16384, true},
	{"o3", 100000, false}, // reasoning-only, no vision
	{"o4-mini", 100000, false}, // reasoning-only, no vision

	// DeepSeek
	{"deepseek-v4-", 384000, false}, // text-only in this codebase

	// Gemini
	{"gemini-2.5-", 65536, true},
	{"gemini-2.0-", 65536, true},
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
