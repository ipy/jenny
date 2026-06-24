package tool

import (
	"testing"
)

func TestPermissionRulesEvaluate(t *testing.T) {
	rules := &PermissionRules{
		Allow: []string{"Read", "Bash(npm run build)", "Bash(go test:*)"},
		Deny:  []string{"Bash(curl:*)", "Write(.env)"},
	}

	tests := []struct {
		name       string
		toolName   string
		primaryArg string
		want       PermissionRuleDecision
	}{
		{name: "bare tool allow", toolName: "Read", primaryArg: "/foo/bar", want: PermissionRuleAllow},
		{name: "exact match allow", toolName: "Bash", primaryArg: "npm run build", want: PermissionRuleAllow},
		{name: "prefix match allow", toolName: "Bash", primaryArg: "go test ./...", want: PermissionRuleAllow},
		{name: "prefix match deny", toolName: "Bash", primaryArg: "curl https://evil.com", want: PermissionRuleDeny},
		{name: "exact match deny", toolName: "Write", primaryArg: ".env", want: PermissionRuleDeny},
		{name: "no match", toolName: "Bash", primaryArg: "echo hello", want: PermissionRuleNoMatch},
		{name: "tool not in rules", toolName: "Grep", primaryArg: "pattern", want: PermissionRuleNoMatch},
		{name: "deny wins over allow", toolName: "Bash", primaryArg: "curl https://evil.com", want: PermissionRuleDeny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rules.Evaluate(tt.toolName, tt.primaryArg)
			if got != tt.want {
				t.Errorf("Evaluate(%q, %q) = %d, want %d", tt.toolName, tt.primaryArg, got, tt.want)
			}
		})
	}
}

func TestPermissionRulesNil(t *testing.T) {
	var rules *PermissionRules
	if got := rules.Evaluate("Bash", "ls"); got != PermissionRuleNoMatch {
		t.Errorf("nil rules should return NoMatch, got %d", got)
	}
}

func TestPermissionRulesEmpty(t *testing.T) {
	rules := &PermissionRules{}
	if !rules.IsEmpty() {
		t.Error("empty rules should report IsEmpty=true")
	}
	if got := rules.Evaluate("Bash", "ls"); got != PermissionRuleNoMatch {
		t.Errorf("empty rules should return NoMatch, got %d", got)
	}
}

func TestPermissionRulesMerge(t *testing.T) {
	global := &PermissionRules{
		Allow: []string{"Read"},
		Deny:  []string{"Bash(rm -rf:*)"},
	}
	project := &PermissionRules{
		Allow: []string{"Bash(npm run build)"},
		Deny:  []string{"Write(.env)"},
	}
	global.Merge(project)

	if len(global.Allow) != 2 {
		t.Errorf("expected 2 allow rules after merge, got %d", len(global.Allow))
	}
	if len(global.Deny) != 2 {
		t.Errorf("expected 2 deny rules after merge, got %d", len(global.Deny))
	}
}

func TestPrimaryArgForTool(t *testing.T) {
	tests := []struct {
		toolName string
		input    map[string]any
		want     string
	}{
		{toolName: "Bash", input: map[string]any{"command": "ls -la"}, want: "ls -la"},
		{toolName: "Read", input: map[string]any{"file_path": "foo.go"}, want: "foo.go"},
		{toolName: "Write", input: map[string]any{"file_path": "bar.go"}, want: "bar.go"},
		{toolName: "Edit", input: map[string]any{"file_path": "baz.go"}, want: "baz.go"},
		{toolName: "Glob", input: map[string]any{"pattern": "*.go"}, want: "*.go"},
		{toolName: "Grep", input: map[string]any{"pattern": "TODO"}, want: "TODO"},
		{toolName: "Unknown", input: map[string]any{"anything": "val"}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := PrimaryArgForTool(tt.toolName, tt.input)
			if got != tt.want {
				t.Errorf("PrimaryArgForTool(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestMatchRuleWildcardSuffix(t *testing.T) {
	rules := &PermissionRules{
		Allow: []string{"Bash(npm *)"},
	}
	if got := rules.Evaluate("Bash", "npm install"); got != PermissionRuleAllow {
		t.Errorf("Bash(npm *) should match 'npm install', got %d", got)
	}
	if got := rules.Evaluate("Bash", "yarn install"); got != PermissionRuleNoMatch {
		t.Errorf("Bash(npm *) should not match 'yarn install', got %d", got)
	}
}
