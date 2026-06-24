package agent

import "testing"

func TestResolveModel(t *testing.T) {
	tests := []struct {
		alias    string
		expected string
	}{
		{alias: "sonnet", expected: "claude-sonnet-4-6"},
		{alias: "opus", expected: "claude-opus-4-7"},
		{alias: "haiku", expected: "claude-haiku-4-5-20251001"},
		{alias: "SONNET", expected: "claude-sonnet-4-6"}, // case insensitive
		{alias: "claude-4", expected: "claude-4"},        // unknown passes through
		{alias: "unknown", expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			result := ResolveModel(tt.alias)
			if result != tt.expected {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.alias, result, tt.expected)
			}
		})
	}
}
