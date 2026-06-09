package api

// NormalizationLog records an action taken by the normalization pipeline.
type NormalizationLog struct {
	Pass    string // Name of the normalization pass
	Message string // Description of what was changed
}

// Capabilities represents the capabilities of the API endpoint.
// For this iteration, all capabilities default to enabled.
type Capabilities struct {
	// SupportsPromptCaching indicates whether the endpoint supports prompt caching.
	SupportsPromptCaching bool
}

// NormalizeMessages is the single gateway for all normalization before JSON serialization.
// It applies universal normalization passes to messages and tools, ensuring compatibility
// across all API providers without requiring provider-specific detection.
//
// Message normalization is handled by the agent package before calling SendMessage.
// This function performs tool normalization and final safety-net transformations.
func NormalizeMessages(messages []Message, tools []ToolParam, caps Capabilities) ([]Message, []ToolParam, []NormalizationLog) {
	var logs []NormalizationLog

	// Normalize tools: inject __arg__ placeholder for empty properties (universal)
	normalizedTools := normalizeToolsUniversal(tools, &logs)

	// Final safety-net: deduplicate tool_results across all messages
	normalizedMessages := make([]Message, len(messages))
	for i, msg := range messages {
		normalizedMessages[i] = msg
		normalizedMessages[i].ToolResults = deduplicateToolResults(msg.ToolResults)
	}

	return normalizedMessages, normalizedTools, logs
}

// normalizeToolsUniversal applies universal normalization to tools.
// Empty input_schema.properties get a __arg__ placeholder to satisfy provider requirements.
func normalizeToolsUniversal(tools []ToolParam, logs *[]NormalizationLog) []ToolParam {
	if len(tools) == 0 {
		return tools
	}

	result := make([]ToolParam, len(tools))
	for i, t := range tools {
		result[i] = t
		// Universal fix: empty properties get a placeholder
		// This was previously MiniMax-specific but is now universal
		if result[i].InputSchema.Properties == nil {
			result[i].InputSchema.Properties = make(map[string]any)
		}
		if len(result[i].InputSchema.Properties) == 0 {
			result[i].InputSchema.Properties["__arg__"] = map[string]any{
				"type":        "string",
				"description": "Placeholder argument for empty schema",
			}
			*logs = append(*logs, NormalizationLog{
				Pass:    "EmptySchemaPlaceholder",
				Message: "Added __arg__ placeholder for tool with empty properties",
			})
		}
	}
	return result
}
