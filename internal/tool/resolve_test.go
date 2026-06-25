package tool

import (
	"context"
	"strings"
	"testing"
)

type stubTool struct{ name string }

func (s stubTool) Name() string                { return s.name }
func (s stubTool) Description() string         { return s.name }
func (s stubTool) InputSchema() map[string]any { return nil }
func (s stubTool) Execute(_ context.Context, _ map[string]any, _ string) (*ToolResult, error) {
	return nil, nil
}

func testTools() []Tool {
	return []Tool{
		stubTool{name: "Read"},
		stubTool{name: "Bash"},
		stubTool{name: "Glob"},
		stubTool{name: "Grep"},
		stubTool{name: "write"},
		stubTool{name: "edit"},
		stubTool{name: "agent"},
		stubTool{name: "web_fetch"},
		stubTool{name: "call_mcp"},
	}
}

func TestResolveToolName_ExactMatch(t *testing.T) {
	resolved, wasResolved := ResolveToolName("Read", testTools())
	if resolved != "Read" || wasResolved {
		t.Fatalf("ResolveToolName() = (%q, %v), want (Read, false)", resolved, wasResolved)
	}
}

func TestResolveToolName_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"read", "Read"},
		{"READ", "Read"},
		{"bash", "Bash"},
		{"BASH", "Bash"},
		{"glob", "Glob"},
		{"grep", "Grep"},
	}
	for _, tc := range tests {
		resolved, wasResolved := ResolveToolName(tc.input, testTools())
		if resolved != tc.expected || !wasResolved {
			t.Errorf("ResolveToolName(%q) = (%q, %v), want (%q, true)", tc.input, resolved, wasResolved, tc.expected)
		}
	}
}

func TestResolveToolName_Aliases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"file_reader", "Read"},
		{"terminal", "Bash"},
		{"shell", "Bash"},
		{"search", "Grep"},
		{"find_files", "Glob"},
		{"write_file", "write"},
		{"edit_file", "edit"},
		{"task", "agent"},
		{"TASK", "agent"},
	}
	for _, tc := range tests {
		resolved, wasResolved := ResolveToolName(tc.input, testTools())
		if resolved != tc.expected || !wasResolved {
			t.Errorf("ResolveToolName(%q) = (%q, %v), want (%q, true)", tc.input, resolved, wasResolved, tc.expected)
		}
	}
}

func TestResolveToolName_HyphenNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"web-fetch", "web_fetch"},
		{"call-mcp", "call_mcp"},
	}
	for _, tc := range tests {
		resolved, wasResolved := ResolveToolName(tc.input, testTools())
		if resolved != tc.expected || !wasResolved {
			t.Errorf("ResolveToolName(%q) = (%q, %v), want (%q, true)", tc.input, resolved, wasResolved, tc.expected)
		}
	}
}

func TestResolveToolName_NotFound(t *testing.T) {
	resolved, wasResolved := ResolveToolName("totally_unknown", testTools())
	if resolved != "" || wasResolved {
		t.Fatalf("ResolveToolName() = (%q, %v), want (\"\", false)", resolved, wasResolved)
	}
}

func TestResolveToolName_AliasTargetMissing(t *testing.T) {
	// agent alias exists but agent tool not in set
	tools := []Tool{stubTool{name: "Read"}}
	resolved, wasResolved := ResolveToolName("task", tools)
	if resolved != "" || wasResolved {
		t.Fatalf("ResolveToolName(task) with no agent = (%q, %v), want (\"\", false)", resolved, wasResolved)
	}
}

func TestFindToolResolved(t *testing.T) {
	tool, resolved, wasResolved := FindToolResolved(testTools(), "read")
	if tool == nil || resolved != "Read" || !wasResolved {
		t.Fatalf("FindToolResolved(read) = (%v, %q, %v), want (Read tool, Read, true)", tool, resolved, wasResolved)
	}
	if tool.Name() != "Read" {
		t.Fatalf("tool.Name() = %q, want Read", tool.Name())
	}
}

func TestFormatUnknownToolError(t *testing.T) {
	msg := FormatUnknownToolError("Reed", testTools())
	if !strings.Contains(msg, "Error: No such tool available: Reed") {
		t.Errorf("missing attempted name in %q", msg)
	}
	if !strings.Contains(msg, "Available tools:") {
		t.Errorf("missing available tools list in %q", msg)
	}
	if !strings.Contains(msg, "Did you mean: Read?") {
		t.Errorf("missing suggestion in %q", msg)
	}
	for _, name := range []string{"Read", "Bash", "Glob"} {
		if !strings.Contains(msg, name) {
			t.Errorf("available list missing %q in %q", name, msg)
		}
	}
}

func TestFormatUnknownToolError_NoSuggestionWhenExact(t *testing.T) {
	msg := FormatUnknownToolError("Read", testTools())
	if strings.Contains(msg, "Did you mean:") {
		t.Errorf("should not suggest when attempted name matches available tool exactly: %q", msg)
	}
}

func TestLevenshteinDistance(t *testing.T) {
	if d := levenshtein("kitten", "sitting"); d != 3 {
		t.Errorf("levenshtein(kitten, sitting) = %d, want 3", d)
	}
}
