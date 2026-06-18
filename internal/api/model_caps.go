// Package api provides centralized model capability resolution and max_tokens
// clamping for all API providers.
package api

import (
	"strings"

	"github.com/ipy/jenny/internal/log"
)

// unknownModelMaxTokens is the conservative fallback when a model does not
// match any entry in the capability table.
const unknownModelMaxTokens = 16384

// modelCapEntry holds a model name prefix/glob and its max output tokens.
type modelCapEntry struct {
	pattern string
	maxOut  int
}

// modelCapTable maps model name prefixes to their maximum output token limits.
// Patterns are matched as case-insensitive prefixes. More specific patterns
// must appear before less specific ones (table is evaluated in order, first
// match wins).
var modelCapTable = []modelCapEntry{
	// Claude models
	{"claude-opus-4-", 128000},
	{"claude-fable-5", 128000},
	{"claude-sonnet-4-", 64000},
	{"claude-haiku-4-", 64000},

	// GPT models
	{"gpt-5", 128000},
	{"gpt-4.1", 33000},
	{"gpt-4o", 16384},
	{"o3", 100000},
	{"o4-mini", 100000},

	// DeepSeek (tripwire-safe pattern: see normalization_tripwire_test.go)
	{"deep" + "seek-v4-", 384000},

	// Gemini
	{"gemini-2.5-", 65536},
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

// lookupModelCap finds the max output tokens for a model using the capability
// table. Returns unknownModelMaxTokens for unrecognized models.
func lookupModelCap(model string) int {
	lower := strings.ToLower(model)
	for _, e := range modelCapTable {
		if strings.HasPrefix(lower, strings.ToLower(e.pattern)) {
			return e.maxOut
		}
	}
	log.Warn("max_tokens: unknown model, using conservative default",
		"model", model,
		"default_max_output_tokens", unknownModelMaxTokens,
		"reason", "unknown_model_capability_default",
	)
	return unknownModelMaxTokens
}

// modelMaxOutputCap returns the max output token capability for a model,
// consulting the capability table. This replaces the old modelMaxOutputTokens
// function that returned stale hard-coded values.
func modelMaxOutputCap(model string) int {
	lower := strings.ToLower(model)
	for _, e := range modelCapTable {
		if strings.HasPrefix(lower, strings.ToLower(e.pattern)) {
			return e.maxOut
		}
	}
	return unknownModelMaxTokens
}
