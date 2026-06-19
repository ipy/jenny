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

	// Merge consecutive same-role messages (universal) — done early so that
	// credential-bound artifact detection and content block validation operate
	// on the collapsed representation.
	messages = MergeConsecutiveSameRole(messages)

	// Strip credential-bound artifacts (redacted_thinking) when key mismatch detected
	messages, stripLogs := stripCredentialBoundArtifacts(messages, caps)
	logs = append(logs, stripLogs...)

	// Final safety-net: deduplicate tool_results across all messages
	normalizedMessages := make([]Message, len(messages))
	for i, msg := range messages {
		normalizedMessages[i] = msg
		normalizedMessages[i].ToolResults = deduplicateToolResults(msg.ToolResults)
	}

	// Content block validation (SSNF Pass 2C): handles edge cases from session resume.
	normalizedMessages, contentLogs := validateContentBlocks(normalizedMessages, &logs)
	logs = append(logs, contentLogs...)

	// Remove non-final tool_use/tool_result pairs where Input is an empty map
	// (originally nil/arguments=null from model). Keeps the last occurrence so
	// the model can see the error and retry, but cleans up earlier failed attempts
	// to avoid context bloat and repeating the same mistake.
	normalizedMessages, emptyInputLogs := removeEmptyInputToolPairsNonFinal(normalizedMessages, &logs)
	logs = append(logs, emptyInputLogs...)

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

// removeEmptyInputToolPairsNonFinal removes tool_use/tool_result pairs from
// non-final messages where the tool_use Input is an empty map (originally nil
// or arguments=null from the model). It keeps the last occurrence so the model
// can see the error and retry, but cleans up earlier failed attempts to avoid
// context bloat and reduce the chance of the model repeating the same mistake.
//
// An empty Input map (len == 0) indicates the model returned no arguments
// (arguments=null, arguments missing, or JSON null), which was converted to
// an empty map by the API-layer fallback. The corresponding tool_result is
// expected to be an error message (e.g. "command is required").
func removeEmptyInputToolPairsNonFinal(messages []Message, logs *[]NormalizationLog) ([]Message, []NormalizationLog) {
	if len(messages) == 0 {
		return messages, nil
	}

	// Collect ToolUseIDs of empty-input tool_use blocks that are NOT in the
	// last assistant+user pair. "Last pair" means the last two messages where
	// the first is an assistant with tool_use and the second is a user with
	// tool_results for those IDs.
	var lastPairToolUseIDs map[string]bool

	// Walk backwards to find the last assistant message with tool_use
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleAssistant && len(messages[i].ToolUse) > 0 {
			lastPairToolUseIDs = make(map[string]bool)
			for _, tu := range messages[i].ToolUse {
				lastPairToolUseIDs[tu.ID] = true
			}
			break
		}
	}

	// Identify ToolUseIDs to remove: empty-input tool_use blocks NOT in last pair
	var removeIDs map[string]bool
	for i := range messages {
		if messages[i].Role != RoleAssistant {
			continue
		}
		for _, tu := range messages[i].ToolUse {
			if isEmptyInput(tu.Input) {
				// Keep if this ID is in the last assistant pair
				if lastPairToolUseIDs != nil && lastPairToolUseIDs[tu.ID] {
					continue
				}
				if removeIDs == nil {
					removeIDs = make(map[string]bool)
				}
				removeIDs[tu.ID] = true
			}
		}
	}

	if removeIDs == nil {
		return messages, nil
	}

	removedCount := 0
	var result []Message

	for _, msg := range messages {
		// Remove matching tool_use blocks from assistant messages
		if msg.Role == RoleAssistant && len(msg.ToolUse) > 0 {
			var filtered []ToolUseBlock
			for _, tu := range msg.ToolUse {
				if !removeIDs[tu.ID] {
					filtered = append(filtered, tu)
				} else {
					removedCount++
				}
			}
			if len(filtered) < len(msg.ToolUse) {
				msg.ToolUse = filtered
				// If only empty-input tool_use blocks were removed and nothing
				// else remains, and content is empty, insert placeholder to
				// avoid empty assistant message (AC2 consistency).
				if len(filtered) == 0 && msg.Content == "" {
					msg.Content = "[No content]"
				}
			}
		}

		// Remove matching tool_result blocks from user messages
		if msg.Role == RoleUser && len(msg.ToolResults) > 0 {
			var filtered []ToolResultBlock
			for _, tr := range msg.ToolResults {
				if !removeIDs[tr.ToolUseID] {
					filtered = append(filtered, tr)
				} else {
					removedCount++
				}
			}
			if len(filtered) < len(msg.ToolResults) {
				msg.ToolResults = filtered
			}
		}

		result = append(result, msg)
	}

	var contentLogs []NormalizationLog
	if removedCount > 0 {
		contentLogs = append(contentLogs, NormalizationLog{
			Pass:    "EmptyInputToolPairCleanup",
			Message: fmt.Sprintf("Removed %d empty-input tool_use/tool_result pair(s) from non-final messages", removedCount/2),
		})
	}

	return result, contentLogs
}

// isEmptyInput checks if a tool input map is empty (originally nil/arguments=null).
func isEmptyInput(input map[string]any) bool {
	if input == nil {
		return true
	}
	return len(input) == 0
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
// - AC4: Drop messages that contain only thinking/redacted_thinking blocks (unconditional)
//
// This function operates on the Message.Content string field using regex patterns.
// The Content field holds the wire-format concatenation of all content blocks (text,
// thinking, redacted_thinking) as produced by the transcript layer. This differs
// from the ContentBlock type used in API responses (client.go Message struct has
// Content string + Thinking string, not []ContentBlock).
func validateContentBlocks(messages []Message, logs *[]NormalizationLog) ([]Message, []NormalizationLog) {
	if len(messages) == 0 {
		return messages, nil
	}

	var contentLogs []NormalizationLog

	// Pass 1: AC4 — Drop messages with only thinking blocks (unconditional per spec)
	messages = dropOrphanedThinkingMessages(messages, &contentLogs)

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
// Per AC4: drop unconditionally — applies regardless of message position or
// any other derived state such as credential-stripping decisions.
func dropOrphanedThinkingMessages(messages []Message, logs *[]NormalizationLog) []Message {
	var result []Message
	droppedCount := 0

	for _, msg := range messages {
		if msg.Role != RoleAssistant {
			result = append(result, msg)
			continue
		}

		if isThinkingOnlyContent(msg.Content) && len(msg.ToolUse) == 0 {
			droppedCount++
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

