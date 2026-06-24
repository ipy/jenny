package agent

import (
	"os"
	"strings"
)

// modelAliases maps model alias names to concrete model identifiers.
// Concrete identifiers are resolved from env vars with hardcoded fallbacks.
var modelAliases = map[string]string{
	"sonnet": getEnvOrDefault("ANTHROPIC_DEFAULT_SONNET_MODEL", "claude-sonnet-4-6"),
	"opus":   getEnvOrDefault("ANTHROPIC_DEFAULT_OPUS_MODEL", "claude-opus-4-7"),
	"haiku":  getEnvOrDefault("ANTHROPIC_DEFAULT_HAIKU_MODEL", "claude-haiku-4-5-20251001"),
}

func getEnvOrDefault(envVar, defaultVal string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return defaultVal
}

// ResolveModel resolves a model alias to its concrete model identifier.
// If the alias is unknown, returns the input unchanged.
func ResolveModel(alias string) string {
	if resolved, ok := modelAliases[strings.ToLower(alias)]; ok {
		return resolved
	}
	return alias
}

// resolvedModelForCost returns the best model identifier for cost tracking.
// Prefers the API response model (always a concrete identifier), falling back
// to ResolveModel(configured) when the response model is empty.
func resolvedModelForCost(configured, responseModel string) string {
	if responseModel != "" {
		return responseModel
	}
	return ResolveModel(configured)
}
