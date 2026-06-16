package api

import (
	"fmt"
	"os"
	"regexp"
)

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
	// OriginalAPIKey is the API key used when the session was created.
	// If non-empty and different from the current ANTHROPIC_API_KEY,
	// credential-bound artifacts (like redacted_thinking blocks) must be stripped.
	OriginalAPIKey string
	// DisableExperimentalBetas when true strips experimental beta fields
	// (defer_loading, cache_control, eager_input_streaming) from tool definitions.
	DisableExperimentalBetas bool
}

// NormalizeMessages is the single gateway for all normalization before JSON serialization.
// It applies universal normalization passes to messages and tools, ensuring compatibility
// across all API providers without requiring provider-specific detection.
//
// Message normalization is handled by the agent package before calling SendMessage.
// This function performs tool normalization and final safety-net transformations.
func NormalizeMessages(messages []Message, tools []ToolParam, caps Capabilities) ([]Message, []ToolParam, []NormalizationLog) {
	var logs []NormalizationLog

	// Normalize tools: tool schema stabilization (SSNF Pass 1)
	// - Injects __arg__ placeholder for empty properties (universal)
	// - Strips experimental beta fields when DisableExperimentalBetas is true
	normalizedTools := NormalizeTools(tools, caps, &logs)

	// Flatten tool_result content (universal)
	messages = flattenToolResultContent(messages)

	// Strip credential-bound artifacts (redacted_thinking) when key mismatch detected
	messages, stripLogs := stripCredentialBoundArtifacts(messages, caps)
	logs = append(logs, stripLogs...)

	// Final safety-net: deduplicate tool_results across all messages
	normalizedMessages := make([]Message, len(messages))
	for i, msg := range messages {
		normalizedMessages[i] = msg
		normalizedMessages[i].ToolResults = deduplicateToolResults(msg.ToolResults)
	}

	// Merge consecutive same-role messages (universal)
	normalizedMessages = MergeConsecutiveSameRole(normalizedMessages)

	return normalizedMessages, normalizedTools, logs
}

// MergeConsecutiveSameRole merges consecutive messages with the same role.
func MergeConsecutiveSameRole(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	var result []Message
	var current *Message

	for _, msg := range messages {
		if current == nil {
			m := msg
			current = &m
			continue
		}

		if current.Role == msg.Role {
			// Merge content
			if msg.Content != "" {
				if current.Content != "" {
					current.Content += "\n"
				}
				current.Content += msg.Content
			}
			// Merge Thinking
			if msg.Thinking != "" {
				if current.Thinking != "" {
					current.Thinking += "\n"
				}
				current.Thinking += msg.Thinking
			}
			// Concatenate tool_use for assistant
			if msg.Role == RoleAssistant {
				current.ToolUse = append(current.ToolUse, msg.ToolUse...)
			}
			// Merge tool_results for user (dedup by ToolUseID - last-writer-wins)
			if msg.Role == RoleUser {
				// Map ToolUseID -> index in current.ToolResults for last-writer-wins
				seenIDToIdx := make(map[string]int)
				for i, tr := range current.ToolResults {
					seenIDToIdx[tr.ToolUseID] = i
				}
				for _, tr := range msg.ToolResults {
					if idx, exists := seenIDToIdx[tr.ToolUseID]; exists {
						// Replace existing entry (last writer wins)
						current.ToolResults[idx] = tr
					} else {
						current.ToolResults = append(current.ToolResults, tr)
						seenIDToIdx[tr.ToolUseID] = len(current.ToolResults) - 1
					}
				}
			}
		} else {
			result = append(result, *current)
			m := msg
			current = &m
		}
	}

	if current != nil {
		result = append(result, *current)
	}

	return result
}

// flattenToolResultContent ensures all tool_result blocks have plain string content.
// This is currently a pass-through because ToolResultBlock.Content is already a string,
// but it serves as the universal hook for ensuring provider-agnostic flattening
// before provider-specific SDK conversion.
func flattenToolResultContent(messages []Message) []Message {
	for i := range messages {
		for j := range messages[i].ToolResults {
			// tr.Content is already a string in our internal representation.
			// This pass ensures it stays that way if we ever support complex content.
			_ = messages[i].ToolResults[j].Content
		}
	}
	return messages
}

// NormalizeTools applies tool schema stabilization (SSNF Pass 1):
// - Injects __arg__ placeholder for tools with empty input_schema
// - Strips experimental beta fields when DisableExperimentalBetas is true
func NormalizeTools(tools []ToolParam, caps Capabilities, logs *[]NormalizationLog) []ToolParam {
	if len(tools) == 0 {
		return tools
	}

	for i := range tools {
		// Strip experimental beta fields when requested
		if caps.DisableExperimentalBetas {
			tools[i] = stripBetaFields(tools[i])
		}
		// Ensure non-empty schema with __arg__ placeholder
		tools[i] = ensureNonEmptySchemaWithLog(tools[i], logs)
	}
	return tools
}

// ensureNonEmptySchema injects __arg__ placeholder for tools with empty input_schema.
func ensureNonEmptySchema(tool ToolParam) ToolParam {
	// Handle nil InputSchema by creating a new one
	if tool.InputSchema.Properties == nil {
		tool.InputSchema.Properties = make(map[string]any)
	}
	// Inject __arg__ if properties are empty
	if len(tool.InputSchema.Properties) == 0 {
		tool.InputSchema.Properties["__arg__"] = map[string]any{
			"type":        "string",
			"description": "Placeholder for tools with no arguments",
		}
	}
	return tool
}

// ensureNonEmptySchemaWithLog is like ensureNonEmptySchema but also logs the change.
func ensureNonEmptySchemaWithLog(tool ToolParam, logs *[]NormalizationLog) ToolParam {
	// Handle nil InputSchema by creating a new one
	if tool.InputSchema.Properties == nil {
		tool.InputSchema.Properties = make(map[string]any)
	}
	// Inject __arg__ if properties are empty
	if len(tool.InputSchema.Properties) == 0 {
		tool.InputSchema.Properties["__arg__"] = map[string]any{
			"type":        "string",
			"description": "Placeholder for tools with no arguments",
		}
		if logs != nil {
			*logs = append(*logs, NormalizationLog{
				Pass:    "EmptySchemaPlaceholder",
				Message: "Added __arg__ placeholder for tool with empty properties",
			})
		}
	}
	return tool
}

// stripBetaFields removes experimental beta fields from tool definitions.
func stripBetaFields(tool ToolParam) ToolParam {
	if tool.InputSchema.ExtraFields == nil {
		return tool
	}
	delete(tool.InputSchema.ExtraFields, "defer_loading")
	delete(tool.InputSchema.ExtraFields, "cache_control")
	delete(tool.InputSchema.ExtraFields, "eager_input_streaming")
	return tool
}

// redactedThinkingPattern matches <thinking type="redacted">...</thinking> blocks.
// The pattern captures the opening tag with type="redacted" and any content up to
// the closing </thinking> tag, handling both single-line and multi-line content.
var redactedThinkingPattern = regexp.MustCompile(`<thinking type="redacted"[^>]*>[\s\S]*?</thinking>`)

// stripCredentialBoundArtifacts removes redacted_thinking blocks from assistant messages
// when the session was resumed with a different API key. These signature-bearing blocks
// are bound to the original key and would cause API 400 errors if sent with a different key.
func stripCredentialBoundArtifacts(messages []Message, caps Capabilities) ([]Message, []NormalizationLog) {
	// Skip if no original key was recorded or if keys match
	if caps.OriginalAPIKey == "" || caps.OriginalAPIKey == os.Getenv("ANTHROPIC_API_KEY") {
		return messages, nil
	}

	var logs []NormalizationLog
	totalStripped := 0

	result := make([]Message, len(messages))
	for i, msg := range messages {
		result[i] = msg

		// Only strip from assistant messages
		if msg.Role != RoleAssistant {
			continue
		}

		original := msg.Content
		stripped := redactedThinkingPattern.ReplaceAllString(msg.Content, "")

		if stripped != original {
			result[i].Content = stripped

			// Count how many blocks were stripped (approximate based on occurrences)
			strippedCount := len(redactedThinkingPattern.FindAllStringIndex(original, -1))
			totalStripped += strippedCount
		}
	}

	if totalStripped > 0 {
		logs = append(logs, NormalizationLog{
			Pass:    "StripCredentialBoundArtifacts",
			Message: fmt.Sprintf("Stripped %d redacted_thinking block(s) from message history", totalStripped),
		})
	}

	return result, logs
}
