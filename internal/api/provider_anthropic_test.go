package api

import (
	"testing"
)

// TestAnthropicProvider_SupportsNativeSearch verifies that SupportsNativeSearch
// returns true unconditionally for the Anthropic provider.
func TestAnthropicProvider_SupportsNativeSearch(t *testing.T) {
	tests := []struct {
		model string
	}{
		{model: "claude-sonnet-4-20250514"},
		{model: "claude-opus-4-20250514"},
		{model: "claude-haiku-4-5-20241022"},
		{model: ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := &anthropicProvider{model: tt.model}
			if !p.SupportsNativeSearch() {
				t.Errorf("SupportsNativeSearch() for model %q = false, want true", tt.model)
			}
		})
	}
}
