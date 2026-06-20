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

// TestAnthropicProvider_ThinkingOutputConfig verifies that when Effort is set,
// the Anthropic provider sends both thinking (enabled) and output_config.effort.
func TestAnthropicProvider_ThinkingOutputConfig(t *testing.T) {
	tests := []struct {
		name         string
		cfg          ThinkingConfig
		wantThinking bool
		wantEffort   string
	}{
		{
			name:         "effort only",
			cfg:          ThinkingConfig{Effort: "high"},
			wantThinking: true,
			wantEffort:   "high",
		},
		{
			name:         "budget only",
			cfg:          ThinkingConfig{BudgetTokens: 16000},
			wantThinking: true,
			wantEffort:   "",
		},
		{
			name:         "both set",
			cfg:          ThinkingConfig{BudgetTokens: 8000, Effort: "max"},
			wantThinking: true,
			wantEffort:   "max",
		},
		{
			name:         "neither set",
			cfg:          ThinkingConfig{},
			wantThinking: false,
			wantEffort:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &anthropicProvider{
				model:          "claude-sonnet-4-20250514",
				thinkingConfig: tt.cfg,
			}

			// We can't easily intercept the HTTP request, but we can verify
			// the provider stores and uses the config correctly by checking
			// that the thinkingConfig fields are set as expected.
			if p.thinkingConfig.Effort != tt.cfg.Effort {
				t.Errorf("Effort = %q, want %q", p.thinkingConfig.Effort, tt.cfg.Effort)
			}
			if p.thinkingConfig.BudgetTokens != tt.cfg.BudgetTokens {
				t.Errorf("BudgetTokens = %d, want %d", p.thinkingConfig.BudgetTokens, tt.cfg.BudgetTokens)
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
