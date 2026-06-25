package agent

import (
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

const maxPinnedMessages = 10

// isCompactionBoundary reports whether a message is a compaction boundary marker.
func isCompactionBoundary(msg api.Message) bool {
	content := msg.Content
	const wrapperOpen = "<system-reminder>\n"
	const wrapperClose = "\n</system-reminder>"
	if strings.HasPrefix(content, wrapperOpen) && strings.HasSuffix(content, wrapperClose) {
		content = content[len(wrapperOpen) : len(content)-len(wrapperClose)]
	}
	return strings.HasPrefix(content, compactSummaryPrefix)
}

// enforcePinnedLimit unpins the oldest non-boundary pinned messages when the count
// exceeds maxPinnedMessages. Boundary markers are never unpinned.
func enforcePinnedLimit(messages []api.Message) []api.Message {
	pinnedCount := 0
	for _, msg := range messages {
		if msg.Pinned {
			pinnedCount++
		}
	}
	if pinnedCount <= maxPinnedMessages {
		return messages
	}

	result := make([]api.Message, len(messages))
	copy(result, messages)
	toUnpin := pinnedCount - maxPinnedMessages
	for i := range result {
		if toUnpin == 0 {
			break
		}
		if result[i].Pinned && !isCompactionBoundary(result[i]) {
			result[i].Pinned = false
			toUnpin--
		}
	}
	return result
}

// formatPinnedSummaryNote builds a note listing pinned messages excluded from summarization.
func formatPinnedSummaryNote(pinned []api.Message) string {
	if len(pinned) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nThe following messages are pinned and preserved verbatim — do not re-summarize their content:\n")
	for i, msg := range pinned {
		summary := pinnedMessageSummary(msg)
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, msg.Role, summary)
	}
	return sb.String()
}

func pinnedMessageSummary(msg api.Message) string {
	content := msg.Content
	const wrapperOpen = "<system-reminder>\n"
	const wrapperClose = "\n</system-reminder>"
	if strings.HasPrefix(content, wrapperOpen) && strings.HasSuffix(content, wrapperClose) {
		content = content[len(wrapperOpen) : len(content)-len(wrapperClose)]
	}
	if isCompactionBoundary(msg) {
		return "[compaction boundary summary]"
	}
	const maxLen = 120
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > maxLen {
		return content[:maxLen] + "..."
	}
	return content
}

// pinnedMessagesForSummary collects pinned messages from a chain for the compaction prompt.
func pinnedMessagesForSummary(messages []api.Message) []api.Message {
	var pinned []api.Message
	for _, msg := range messages {
		if msg.Pinned {
			pinned = append(pinned, msg)
		}
	}
	return pinned
}
