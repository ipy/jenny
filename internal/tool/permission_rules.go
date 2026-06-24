package tool

import (
	"strings"
)

// PermissionRules holds allow and deny rules loaded from config.
type PermissionRules struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// PermissionRuleDecision is the result of evaluating permission rules.
type PermissionRuleDecision int

const (
	// PermissionRuleNoMatch means no rule matched; fall through to default behavior.
	PermissionRuleNoMatch PermissionRuleDecision = iota
	// PermissionRuleAllow means an allow rule matched.
	PermissionRuleAllow
	// PermissionRuleDeny means a deny rule matched.
	PermissionRuleDeny
)

// Evaluate checks the rules against a tool invocation.
// Deny rules are checked first (deny wins over allow).
// Returns the decision.
func (r *PermissionRules) Evaluate(toolName string, primaryArg string) PermissionRuleDecision {
	if r == nil {
		return PermissionRuleNoMatch
	}
	for _, rule := range r.Deny {
		if matchRule(rule, toolName, primaryArg) {
			return PermissionRuleDeny
		}
	}
	for _, rule := range r.Allow {
		if matchRule(rule, toolName, primaryArg) {
			return PermissionRuleAllow
		}
	}
	return PermissionRuleNoMatch
}

// IsEmpty returns true if there are no rules.
func (r *PermissionRules) IsEmpty() bool {
	return r == nil || (len(r.Allow) == 0 && len(r.Deny) == 0)
}

// Merge appends rules from other into r.
func (r *PermissionRules) Merge(other *PermissionRules) {
	if other == nil {
		return
	}
	r.Allow = append(r.Allow, other.Allow...)
	r.Deny = append(r.Deny, other.Deny...)
}

// matchRule checks if a rule pattern matches a tool invocation.
// Patterns: "ToolName", "ToolName(exact)", "ToolName(prefix:*)".
func matchRule(rule, toolName, primaryArg string) bool {
	parenIdx := strings.IndexByte(rule, '(')
	if parenIdx < 0 {
		return rule == toolName
	}

	ruleTool := rule[:parenIdx]
	if ruleTool != toolName {
		return false
	}

	if !strings.HasSuffix(rule, ")") {
		return false
	}
	pattern := rule[parenIdx+1 : len(rule)-1]

	if strings.HasSuffix(pattern, ":*") {
		prefix := pattern[:len(pattern)-2]
		return strings.HasPrefix(primaryArg, prefix)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(primaryArg, prefix)
	}
	return pattern == primaryArg
}

// PrimaryArgForTool extracts the primary argument from a tool's input parameters.
func PrimaryArgForTool(toolName string, input map[string]any) string {
	key := primaryArgKey(toolName)
	if key == "" {
		return ""
	}
	if v, ok := input[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func primaryArgKey(toolName string) string {
	switch toolName {
	case "Bash":
		return "command"
	case "Read":
		return "file_path"
	case "Write":
		return "file_path"
	case "Edit":
		return "file_path"
	case "Glob":
		return "pattern"
	case "Grep":
		return "pattern"
	case "WebFetch":
		return "url"
	case "WebSearch":
		return "query"
	case "Task":
		return "prompt"
	default:
		return ""
	}
}
