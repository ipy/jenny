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

// Package-level compiled regex patterns — compiled once at package init, not per-call.
var (
	// thinkingPattern matches any <thinking>...</thinking> block (including typed variants).
	thinkingPattern = regexp.MustCompile(`<thinking(\s+type="[^"]*")?>[\s\S]*?</thinking>`)
	// whitespaceOnlyPattern matches strings that contain only whitespace.
	whitespaceOnlyPattern = regexp.MustCompile(`^\s*$`)
	// redactedThinkingPattern matches <thinking type="redacted">...</thinking> blocks.
	redactedThinkingPattern = regexp.MustCompile(`<thinking type="redacted"[^>]*>[\s\S]*?</thinking>`)
)

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

	// Build mixedThinkingMap from pre-strip content. A message is "mixed" if it
	// contains both redacted and non-redacted thinking. This must be done before
	// stripCredentialBoundArtifacts because that pass removes redacted blocks,
	// making it impossible to detect original mixed content afterward.
	// Indices align with the post-merge slice: merge happens after this map is
	// built, so a merged message at index j inherits the map entry of its first
	// component (the slot that survives the merge).
	mixedThinkingMap := make(map[int]bool)
	for i, msg := range messages {
		if msg.Role == RoleAssistant {
			trimmed := strings.TrimSpace(msg.Content)
			afterStrippingRedacted := redactedThinkingPattern.ReplaceAllString(trimmed, "")
			if strings.TrimSpace(afterStrippingRedacted) != "" && redactedThinkingPattern.MatchString(trimmed) {
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

	// Content block validation (SSNF Pass 2C): handles edge cases from session resume.
	// strippingWasSkipped is true when OriginalAPIKey matches current key (no stripping occurred).
	// In that case, redacted_thinking is valid and must not be dropped by AC4.
	strippingWasSkipped := caps.OriginalAPIKey != "" && len(stripLogs) == 0
	normalizedMessages, contentLogs := validateContentBlocks(normalizedMessages, &logs, mixedThinkingMap, strippingWasSkipped)
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
//
// This function operates on the Message.Content string field using regex patterns.
// The Content field holds the wire-format concatenation of all content blocks (text,
// thinking, redacted_thinking) as produced by the transcript layer. This differs
// from the ContentBlock type used in API responses (client.go).
//
// Parameters:
//   - mixedThinkingMap: built pre-strip/pre-merge; marks messages that originally had both
//     redacted and non-redacted thinking (these must be preserved so AC3 strips trailing).
//   - strippingWasSkipped: true when credential stripping was skipped (API keys matched);
//     in that case, redacted_thinking is valid and must not be dropped by AC4.
func validateContentBlocks(messages []Message, logs *[]NormalizationLog, mixedThinkingMap map[int]bool, strippingWasSkipped bool) ([]Message, []NormalizationLog) {
	if len(messages) == 0 {
		return messages, nil
	}

	var contentLogs []NormalizationLog

	// Pass 1: AC4 — Drop messages with only thinking blocks
	messages = dropOrphanedThinkingMessages(messages, &contentLogs, mixedThinkingMap, strippingWasSkipped)

	// Pass 2: AC3 — Strip trailing thinking/redacted_thinking blocks
	messages = stripTrailingThinkingBlocks(messages, &contentLogs)

	// Pass 3: AC1 — Strip whitespace-only text content blocks
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

	// Pass 4: AC2 — Insert placeholder for empty content in non-final assistant messages
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
// Per AC4: drop any assistant message containing only thinking/redacted_thinking blocks.
// The mixedThinkingMap indicates messages that originally had both redacted and
// non-redacted thinking — these are preserved so AC3 can strip the trailing thinking.
// strippingWasSkipped is true when no credential stripping occurred (keys matched);
// in that case, redacted_thinking is valid and must not be dropped.
func dropOrphanedThinkingMessages(messages []Message, logs *[]NormalizationLog, mixedThinkingMap map[int]bool, strippingWasSkipped bool) []Message {
	var result []Message
	droppedCount := 0

	for i, msg := range messages {
		if msg.Role != RoleAssistant {
			result = append(result, msg)
			continue
		}

		content := msg.Content
		if isThinkingOnlyContent(content) && len(msg.ToolUse) == 0 {
			// If stripping was skipped (keys match), redacted_thinking is valid — preserve.
			if strippingWasSkipped {
				result = append(result, msg)
				continue
			}
			// Stripping was performed: check if message originally had mixed thinking.
			if mixedThinkingMap[i] {
				// Originally had non-redacted + redacted — preserve for AC3 to strip trailing.
				result = append(result, msg)
			} else {
				// Pure thinking-only message — drop per AC4.
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

// isThinkingOnlyContent checks if content consists solely of thinking blocks.
// Uses the package-level thinkingPattern (compiled once at package init).
func isThinkingOnlyContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	stripped := thinkingPattern.ReplaceAllString(trimmed, "")
	return strings.TrimSpace(stripped) == ""
}

// stripTrailingThinkingBlocks removes thinking/redacted_thinking blocks
// from the end of assistant message content strings.
// Uses the package-level thinkingPattern (compiled once at package init).
func stripTrailingThinkingBlocks(messages []Message, logs *[]NormalizationLog) []Message {
	strippedCount := 0

	for i := range messages {
		if messages[i].Role != RoleAssistant || messages[i].Content == "" {
			continue
		}

		original := messages[i].Content
		stripped := stripTrailingThinking(messages[i].Content)

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

// stripTrailingThinking removes trailing thinking blocks from content string.
// Uses the package-level thinkingPattern (compiled once at package init).
func stripTrailingThinking(content string) string {
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
		nonThinkingContent := thinkingPattern.ReplaceAllString(prefix, "")
		if strings.TrimSpace(nonThinkingContent) == "" {
			// Nothing but more thinking blocks before this one - don't strip
			break
		}

		// There's actual content before this thinking block - strip it
		result = trimmed[:startIdx]
	}

	return result
}