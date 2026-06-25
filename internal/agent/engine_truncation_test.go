package agent

import (
	"testing"

	"github.com/ipy/jenny/internal/api"
)

func TestShouldRetryTruncatedToolInput(t *testing.T) {
	tests := []struct {
		name   string
		result api.StreamResult
		blocks []api.ToolUseBlock
		want   bool
	}{
		{
			name:   "not truncated",
			result: api.StreamResult{StopReason: api.StopReasonToolUse, StreamComplete: true},
			blocks: []api.ToolUseBlock{{InputParseError: "fail"}},
			want:   false,
		},
		{
			name:   "max tokens parse error",
			result: api.StreamResult{StopReason: api.StopReasonMaxTokens, StreamComplete: true},
			blocks: []api.ToolUseBlock{{InputParseError: "could not parse"}},
			want:   true,
		},
		{
			name:   "incomplete stream empty input",
			result: api.StreamResult{StreamComplete: false},
			blocks: []api.ToolUseBlock{{Input: map[string]any{}}},
			want:   true,
		},
		{
			name:   "max tokens valid input",
			result: api.StreamResult{StopReason: api.StopReasonMaxTokens, StreamComplete: true},
			blocks: []api.ToolUseBlock{{Input: map[string]any{"command": "ls"}}},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryTruncatedToolInput(&tt.result, tt.blocks); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
