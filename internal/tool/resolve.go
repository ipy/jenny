package tool

import (
	"fmt"
	"strings"
)

// toolNameAliases maps informal or incorrect tool names to canonical registered names.
// Keys are matched case-insensitively.
var toolNameAliases = map[string]string{
	"file_reader": "Read",
	"terminal":    "Bash",
	"shell":       "Bash",
	"search":      "Grep",
	"find_files":  "Glob",
	"write_file":  "write",
	"edit_file":   "edit",
	"task":        "agent",
}

// ResolveToolName attempts to resolve a tool name against a list of available tools.
// Returns the resolved tool name and whether resolution was needed (false = exact match).
// When resolution fails, resolvedName is empty and wasResolved is false.
func ResolveToolName(name string, available []Tool) (resolvedName string, wasResolved bool) {
	if name == "" || len(available) == 0 {
		return "", false
	}

	if resolved, ok := resolveToolNameOnce(name, available); ok {
		return resolved, resolved != name
	}

	normalized := normalizeToolNameSeparators(name)
	if normalized != name {
		if resolved, ok := resolveToolNameOnce(normalized, available); ok {
			return resolved, true
		}
	}

	return "", false
}

func resolveToolNameOnce(name string, available []Tool) (string, bool) {
	for _, t := range available {
		if t.Name() == name {
			return t.Name(), true
		}
	}

	for _, t := range available {
		if strings.EqualFold(t.Name(), name) {
			return t.Name(), true
		}
	}

	for alias, target := range toolNameAliases {
		if strings.EqualFold(alias, name) {
			if canonical, ok := matchRegisteredName(target, available); ok {
				return canonical, true
			}
		}
	}

	return "", false
}

func matchRegisteredName(candidate string, available []Tool) (string, bool) {
	for _, t := range available {
		if t.Name() == candidate || strings.EqualFold(t.Name(), candidate) {
			return t.Name(), true
		}
	}
	return "", false
}

func normalizeToolNameSeparators(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// FindToolResolved resolves a tool name and returns the matching tool, resolved name,
// and whether non-exact resolution was applied.
func FindToolResolved(tools []Tool, name string) (Tool, string, bool) {
	resolved, wasResolved := ResolveToolName(name, tools)
	if resolved == "" {
		return nil, name, false
	}
	return FindTool(tools, resolved), resolved, wasResolved
}

// ToolNames returns registered names for all tools in order.
func ToolNames(tools []Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return names
}

// FormatUnknownToolError builds a diagnostic error when tool resolution fails.
func FormatUnknownToolError(attempted string, available []Tool) string {
	names := ToolNames(available)
	msg := fmt.Sprintf("Error: No such tool available: %s\nAvailable tools: %s", attempted, strings.Join(names, ", "))
	if closest := closestToolName(attempted, names); closest != "" && !strings.EqualFold(attempted, closest) {
		msg += fmt.Sprintf("\nDid you mean: %s?", closest)
	}
	return msg
}

func closestToolName(attempted string, available []string) string {
	if len(available) == 0 {
		return ""
	}
	attemptedLower := strings.ToLower(attempted)
	best := available[0]
	bestDist := levenshtein(attemptedLower, strings.ToLower(best))
	for _, name := range available[1:] {
		d := levenshtein(attemptedLower, strings.ToLower(name))
		if d < bestDist {
			bestDist = d
			best = name
		}
	}
	return best
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
