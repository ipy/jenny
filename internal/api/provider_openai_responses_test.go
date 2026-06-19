package api

import (
	"testing"
)

// TestOpenAIResponsesProvider_SupportsNativeSearch verifies that SupportsNativeSearch
// returns true unconditionally for the OpenAI Responses API provider.
func TestOpenAIResponsesProvider_SupportsNativeSearch(t *testing.T) {
	tests := []struct {
		model string
	}{
		{model: "gpt-4o"},
		{model: "gpt-5"},
		{model: "o3"},
		{model: ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := &openAIResponsesProvider{model: tt.model}
			if !p.SupportsNativeSearch() {
				t.Errorf("SupportsNativeSearch() for model %q = false, want true", tt.model)
			}
		})
	}
}
