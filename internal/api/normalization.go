package api

import (
	"fmt"
	"os"
	"regexp"
	"strings"
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

	// Track which messages originally had mixed thinking (both redacted and non-redacted)
	// This must be done BEFORE stripCredentialBoundArtifacts because that function removes
	// redacted thinking, and we need to know if the original message had both types.
	mixedThinkingMap := make(map[int]bool)
	for i, msg := range messages {
		if msg.Role == RoleAssistant {
			trimmed := strings.TrimSpace(msg.Content)
			afterStrippingRedacted := redactedThinkingPattern.ReplaceAllString(trimmed, "")
			if strings.TrimSpace(afterStrippingRedacted) != "" && containsRedactedThinking(msg.Content) {
				mixedThinkingMap[i] = true
			}
		}
	}

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

	// Content block validation (SSNF Pass 2C): handles edge cases from session resume
	// Determine if credential stripping was applicable and performed:
	// - If caps.OriginalAPIKey != "" and keys match: stripping skipped, preserve redacted_thinking
	// - If caps.OriginalAPIKey == "" or keys don't match: stripping performed or N/A, drop orphaned thinking
	strippingWasSkipped := caps.OriginalAPIKey != "" && len(stripLogs) == 0
	normalizedMessages, contentLogs := validateContentBlocks(normalizedMessages, &logs, strippingWasSkipped, mixedThinkingMap)
	logs = append(logs, contentLogs...)

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

// ensureNonEmptySchemaWithLog ensures the tool has a valid schema with __arg__ placeholder
// when no properties are defined, and sets Required to ["__arg__"] accordingly.
func ensureNonEmptySchemaWithLog(tool ToolParam, logs *[]NormalizationLog) ToolParam {
	// Handle nil Properties by initializing the map
	if tool.InputSchema.Properties == nil {
		tool.InputSchema.Properties = make(map[string]any)
	}

	// Inject __arg__ if properties are empty
	if len(tool.InputSchema.Properties) == 0 {
		tool.InputSchema.Properties["__arg__"] = map[string]any{
			"type":        "string",
			"description": "Placeholder for tools with no arguments",
		}
		tool.InputSchema.Required = []string{"__arg__"}
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

// validateContentBlocks applies SSNF Pass 2C: Content Block Validation.
// It handles edge cases from session resume:
// - AC1: Strip whitespace-only text blocks
// - AC2: Insert placeholder for empty content in non-final assistant messages
// - AC3: Strip trailing thinking/redacted_thinking blocks
// - AC4: Drop messages that contain only thinking/redacted_thinking blocks
// The stripSkipped parameter indicates whether credential-bound artifact stripping
// was skipped (keys match). When true, we preserve redacted_thinking messages.
func validateContentBlocks(messages []Message, logs *[]NormalizationLog, stripSkipped bool, mixedThinkingMap map[int]bool) ([]Message, []NormalizationLog) {
	if len(messages) == 0 {
		return messages, nil
	}

	var contentLogs []NormalizationLog

	// Patterns for parsing content blocks from string content
	thinkingPattern := regexp.MustCompile(`<thinking(\s+type="[^"]*")?>[\s\S]*?</thinking>`)
	whitespaceOnlyPattern := regexp.MustCompile(`^\s*$`)

	// Pass 1: AC4 - Drop messages with only thinking blocks (before other processing)
	// Pass stripSkipped to preserve redacted_thinking when keys match (stripping was skipped).
	messages = dropOrphanedThinkingMessages(messages, &contentLogs, thinkingPattern, stripSkipped, mixedThinkingMap)

	// Pass 2: AC3 - Strip trailing thinking/redacted_thinking blocks
	messages = stripTrailingThinkingBlocks(messages, &contentLogs, thinkingPattern)
	// Pass 3: AC1 - Strip whitespace-only text content blocks
	for i := range messages {
		if messages[i].Role != RoleAssistant {
			continue
		}
		content := messages[i].Content
		if content != "" && whitespaceOnlyPattern.MatchString(content) {
			messages[i].Content = ""
			contentLogs = append(contentLogs, NormalizationLog{
				Pass:    "ContentBlockValidation",
				Message: "Stripped whitespace-only text block",
			})
		}
	}

	// Pass 4: AC2 - Insert placeholder for empty content in non-final assistant messages
	for i := 0; i < len(messages)-1; i++ {
		if messages[i].Role == RoleAssistant && messages[i].Content == "" && len(messages[i].ToolUse) == 0 {
			messages[i].Content = "[No content]"
			contentLogs = append(contentLogs, NormalizationLog{
				Pass:    "ContentBlockValidation",
				Message: "Inserted [No content] placeholder for empty assistant message",
			})
		}
	}

	return messages, contentLogs
}

// dropOrphanedThinkingMessages removes assistant messages that contain only
// thinking and/or redacted_thinking blocks (with no text or tool_use).
// The stripSkipped parameter indicates whether credential-bound artifact stripping
// was skipped (keys match). When true, we preserve redacted_thinking messages.
// The mixedThinkingMap indicates which messages originally had both non-redacted and redacted thinking.
func dropOrphanedThinkingMessages(messages []Message, logs *[]NormalizationLog, pattern *regexp.Regexp, stripSkipped bool, mixedThinkingMap map[int]bool) []Message {
	var result []Message
	droppedCount := 0

	// Pattern to match redacted_thinking blocks
	redactedOnlyPattern := regexp.MustCompile(`<thinking type="redacted"[^>]*>[\s\S]*?</thinking>`)

	for i, msg := range messages {
		if msg.Role != RoleAssistant {
			result = append(result, msg)
			continue
		}

		content := msg.Content
		if isThinkingOnlyContent(content, pattern) && len(msg.ToolUse) == 0 {
			// Check if this message has ONLY redacted thinking blocks
			trimmed := strings.TrimSpace(content)
			afterStrippingRedacted := redactedOnlyPattern.ReplaceAllString(trimmed, "")
			if strings.TrimSpace(afterStrippingRedacted) == "" {
				// Only redacted thinking blocks
				if stripSkipped {
					// Stripping was skipped (keys match) - preserve the message
					// because the redacted_thinking is not credential-bound in this case
					result = append(result, msg)
				} else {
					// Stripping was performed (keys didn't match or N/A) - drop orphaned message
					droppedCount++
				}
				continue
			}
			// Has non-redacted thinking content
			if mixedThinkingMap[i] {
				// Message originally had both non-redacted and redacted thinking.
				// After stripping redacted, preserve for AC3 to handle trailing thinking.
				result = append(result, msg)
			} else {
				// Message originally had only non-redacted thinking - drop it (AC4)
				droppedCount++
			}
			continue
		}

		result = append(result, msg)
	}

	if droppedCount > 0 {
		*logs = append(*logs, NormalizationLog{
			Pass:    "ContentBlockValidation",
			Message: fmt.Sprintf("Dropped %d message(s) with only thinking blocks", droppedCount),
		})
	}

	return result
}

// containsRedactedThinking checks if content contains redacted_thinking blocks.
func containsRedactedThinking(content string) bool {
	return redactedThinkingPattern.MatchString(content)
}

// stripTrailingThinkingBlocks removes thinking/redacted_thinking blocks
// from the end of assistant message content strings.
func stripTrailingThinkingBlocks(messages []Message, logs *[]NormalizationLog, pattern *regexp.Regexp) []Message {
	strippedCount := 0

	for i := range messages {
		if messages[i].Role != RoleAssistant || messages[i].Content == "" {
			continue
		}

		original := messages[i].Content
		stripped := stripTrailingThinking(messages[i].Content, pattern)

		if stripped != original {
			messages[i].Content = stripped
			strippedCount++
		}
	}

	if strippedCount > 0 {
		*logs = append(*logs, NormalizationLog{
			Pass:    "ContentBlockValidation",
			Message: fmt.Sprintf("Stripped trailing thinking block(s) from %d message(s)", strippedCount),
		})
	}

	return messages
}

// isThinkingOnlyContent checks if content consists solely of thinking blocks.
func isThinkingOnlyContent(content string, pattern *regexp.Regexp) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}

	stripped := pattern.ReplaceAllString(trimmed, "")
	return strings.TrimSpace(stripped) == ""
}

// stripTrailingThinking removes trailing thinking blocks from content string.
func stripTrailingThinking(content string, pattern *regexp.Regexp) string {
	result := content

	for {
		trimmed := strings.TrimRight(result, " \t\n\r")
		if trimmed == "" {
			return ""
		}

		if !strings.HasSuffix(trimmed, "</thinking>") {
			break
		}

		endIdx := len(trimmed) - len("</thinking>")
		startIdx := strings.LastIndex(trimmed[:endIdx], "<thinking")
		if startIdx == -1 {
			break
		}

		// Check if there's actual content before this thinking block
		prefix := trimmed[:startIdx]
		nonThinkingContent := pattern.ReplaceAllString(prefix, "")
		if strings.TrimSpace(nonThinkingContent) == "" {
			// Nothing but more thinking blocks before this one - don't strip
			break
		}

		// There's actual content before this thinking block - strip it
		result = trimmed[:startIdx]
	}

	return result
}
