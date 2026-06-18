package api

import (
	"testing"
)

// TestResolveMaxTokens_OverrideWithinCapability verifies that when the override
// is within the model's capability, it is returned unchanged.
func TestResolveMaxTokens_OverrideWithinCapability(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		override int
		want     int
	}{
		{"claude-sonnet-below-cap", "claude-sonnet-4-20250514", 32000, 32000},
		{"claude-sonnet-at-cap", "claude-sonnet-4-20250514", 64000, 64000},
		{"gpt-5-below-cap", "gpt-5.1", 64000, 64000},
		{"gpt-5-at-cap", "gpt-5.1", 128000, 128000},
		{"o3-below-cap", "o3-mini", 50000, 50000},
		{"deepseek-below-cap", "deepseek-v4-pro", 64000, 64000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveMaxTokens(tt.model, tt.override)
			if got != tt.want {
				t.Errorf("ResolveMaxTokens(%q, %d) = %d, want %d", tt.model, tt.override, got, tt.want)
			}
		})
	}
}

// TestResolveMaxTokens_OverrideExceedsCapability verifies clamping when the
// override exceeds the model's capability.
func TestResolveMaxTokens_OverrideExceedsCapability(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		override int
		want     int
	}{
		{"claude-sonnet-exceeds", "claude-sonnet-4-20250514", 128000, 64000},
		{"claude-haiku-exceeds", "claude-haiku-4-5-20251101", 100000, 64000},
		{"gpt-4o-exceeds", "gpt-4o", 64000, 16384},
		{"o4-mini-exceeds", "o4-mini", 128000, 100000},
		{"gemini-exceeds", "gemini-2.5-pro", 128000, 65536},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveMaxTokens(tt.model, tt.override)
			if got != tt.want {
				t.Errorf("ResolveMaxTokens(%q, %d) = %d, want %d (should clamp)", tt.model, tt.override, got, tt.want)
			}
		})
	}
}

// TestResolveMaxTokens_DefaultCase verifies that override=0 returns the full capability.
func TestResolveMaxTokens_DefaultCase(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"claude-opus-4-5-20251101", 128000},
		{"claude-sonnet-4-20250514", 64000},
		{"claude-haiku-4-5-20251101", 64000},
		{"claude-fable-5-1", 128000},
		{"gpt-5.1", 128000},
		{"gpt-4.1", 33000},
		{"gpt-4o", 16384},
		{"o3", 100000},
		{"o4-mini", 100000},
		{"deepseek-v4-flash", 384000},
		{"deepseek-v4-pro", 384000},
		{"gemini-2.5-pro", 65536},
		{"gemini-2.5-flash", 65536},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := ResolveMaxTokens(tt.model, 0)
			if got != tt.want {
				t.Errorf("ResolveMaxTokens(%q, 0) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

// TestResolveMaxTokens_NegativeOverride verifies negative overrides are treated like 0.
func TestResolveMaxTokens_NegativeOverride(t *testing.T) {
	got := ResolveMaxTokens("claude-sonnet-4-20250514", -1)
	want := 64000
	if got != want {
		t.Errorf("ResolveMaxTokens(%q, -1) = %d, want %d", "claude-sonnet-4-20250514", got, want)
	}
}

// TestResolveMaxTokens_UnknownModel verifies conservative fallback for unknown models.
func TestResolveMaxTokens_UnknownModel(t *testing.T) {
	got := ResolveMaxTokens("unknown-model-v42", 0)
	want := 16384
	if got != want {
		t.Errorf("ResolveMaxTokens(%q, 0) = %d, want %d", "unknown-model-v42", got, want)
	}

	// Even with an override, unknown model caps at fallback
	got2 := ResolveMaxTokens("some-random-model", 64000)
	want2 := 16384
	if got2 != want2 {
		t.Errorf("ResolveMaxTokens(%q, 64000) = %d, want %d", "some-random-model", got2, want2)
	}
}

// TestCapabilityTable_AllValuesInRange verifies that all capability table values
// are between 1024 and 400000 inclusive.
func TestCapabilityTable_AllValuesInRange(t *testing.T) {
	for _, e := range modelCapTable {
		if e.maxOut < 1024 {
			t.Errorf("model pattern %q has maxOut %d < 1024", e.pattern, e.maxOut)
		}
		if e.maxOut > 400000 {
			t.Errorf("model pattern %q has maxOut %d > 400000", e.pattern, e.maxOut)
		}
	}
	if unknownModelMaxTokens < 1024 {
		t.Errorf("unknownModelMaxTokens %d < 1024", unknownModelMaxTokens)
	}
	if unknownModelMaxTokens > 400000 {
		t.Errorf("unknownModelMaxTokens %d > 400000", unknownModelMaxTokens)
	}
}

// TestCapabilityTable_DeepSeekRegressionGuard verifies DeepSeek V4 models
// return 384000, not the stale 8192.
func TestCapabilityTable_DeepSeekRegressionGuard(t *testing.T) {
	got := modelMaxOutputCap("deepseek-v4-flash")
	if got != 384000 {
		t.Errorf("modelMaxOutputCap(deepseek-v4-flash) = %d, want 384000", got)
	}

	got = ResolveMaxTokens("deepseek-v4-pro", 0)
	if got != 384000 {
		t.Errorf("ResolveMaxTokens(deepseek-v4-pro, 0) = %d, want 384000", got)
	}

	got = ResolveMaxTokens("deepseek-v4-flash", 8192)
	if got != 8192 {
		t.Errorf("ResolveMaxTokens(deepseek-v4-flash, 8192) = %d, want 8192 (within cap)", got)
	}
}

// TestCapabilityTable_UnknownModelReturnsFallback verifies the catch-all.
func TestCapabilityTable_UnknownModelReturnsFallback(t *testing.T) {
	unknownModels := []string{"", "bogus", "llama-3-70b", "mistral-large"}
	for _, m := range unknownModels {
		got := modelMaxOutputCap(m)
		if got != unknownModelMaxTokens {
			t.Errorf("modelMaxOutputCap(%q) = %d, want %d", m, got, unknownModelMaxTokens)
		}
	}
}

// TestCapabilityTable_CaseInsensitive verifies pattern matching is case-insensitive.
func TestCapabilityTable_CaseInsensitive(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"CLAUDE-SONNET-4-20250514", 64000},
		{"Claude-Opus-4-5-20251101", 128000},
		{"GPT-5.1", 128000},
		{"Gemini-2.5-Pro", 65536},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := modelMaxOutputCap(tt.model)
			if got != tt.want {
				t.Errorf("modelMaxOutputCap(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}
