package tool

import "strings"

// formatToolError builds a structured tool error message for LLM consumption.
func formatToolError(category, reason, input, unsupported, hint string) string {
	var b strings.Builder
	b.WriteString("error: ")
	b.WriteString(category)
	b.WriteString("\n")
	if reason != "" {
		b.WriteString("reason: ")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	if input != "" {
		if len(input) > 200 {
			input = input[:200]
		}
		b.WriteString("input: ")
		b.WriteString(input)
		b.WriteString("\n")
	}
	if unsupported != "" {
		b.WriteString("unsupported: ")
		b.WriteString(unsupported)
		b.WriteString("\n")
	}
	if hint != "" {
		b.WriteString("hint: ")
		b.WriteString(hint)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
