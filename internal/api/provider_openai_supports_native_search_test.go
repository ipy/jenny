package api

import (
	"testing"
)

// TestOpenAIProvider_SupportsNativeSearch verifies that SupportsNativeSearch
// returns true for non-DeepSeek models and false for DeepSeek models routed
// through the OpenAI-compatible provider.
func TestOpenAIProvider_SupportsNativeSearch(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		// Non-DeepSeek models should return true
		{model: "gpt-4o", want: true},
		{model: "gpt-5", want: true},
		{model: "o3", want: true},
		{model: "o4-mini", want: true},
		{model: "chatgpt-4o-latest", want: true},
		{model: "moonshot-v1", want: true},
		{model: "glm-4", want: true},
		{model: "qwen-max", want: true},
		{model: "", want: true},
		{model: "unknown-model", want: true},

		// DeepSeek models should return false
		{model: "deepseek-chat", want: false},
		{model: "deepseek-reasoner", want: false},
		{model: "DeepSeek-V3", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := &openAIProvider{model: tt.model}
			got := p.SupportsNativeSearch()
			if got != tt.want {
				t.Errorf("SupportsNativeSearch() for model %q = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
