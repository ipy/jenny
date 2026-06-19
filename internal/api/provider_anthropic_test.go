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

// TestCategorizeMaxTokensError verifies that categorizeMaxTokensError
// always populates MaxOutputTokens regardless of error category.
func TestCategorizeMaxTokensError(t *testing.T) {
	model := "claude-sonnet-4-20250514"
	outputTokens := 64000

	t.Run("context_rejected", func(t *testing.T) {
		err := categorizeMaxTokensError(model, outputTokens, true)
		if err.Category != CategoryContextExhausted {
			t.Errorf("Category = %q, want %q", err.Category, CategoryContextExhausted)
		}
		if err.Model != model {
			t.Errorf("Model = %q, want %q", err.Model, model)
		}
		if err.OutputTokens != outputTokens {
			t.Errorf("OutputTokens = %d, want %d", err.OutputTokens, outputTokens)
		}
		if err.MaxOutputTokens == 0 {
			t.Error("MaxOutputTokens should be non-zero for context_exhausted category")
		}
	})

	t.Run("output_cap_hit", func(t *testing.T) {
		err := categorizeMaxTokensError(model, outputTokens, false)
		if err.Category != CategoryOutputCapHit {
			t.Errorf("Category = %q, want %q", err.Category, CategoryOutputCapHit)
		}
		if err.Model != model {
			t.Errorf("Model = %q, want %q", err.Model, model)
		}
		if err.OutputTokens != outputTokens {
			t.Errorf("OutputTokens = %d, want %d", err.OutputTokens, outputTokens)
		}
		if err.MaxOutputTokens == 0 {
			t.Error("MaxOutputTokens should be non-zero for output_cap_hit category")
		}
	})

	t.Run("unknown_model", func(t *testing.T) {
		err := categorizeMaxTokensError("unknown-model", 100, true)
		if err.MaxOutputTokens == 0 {
			t.Error("MaxOutputTokens should be non-zero even for unknown models")
		}
		if err.Category != CategoryContextExhausted {
			t.Errorf("Category = %q, want %q", err.Category, CategoryContextExhausted)
		}
	})
}
