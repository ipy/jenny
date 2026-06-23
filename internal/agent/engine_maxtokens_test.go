package agent

import (
	"testing"
)

func TestComplexityResult_OverallComplexity(t *testing.T) {
	cr := ComplexityResult{ReasoningDepth: 3, CodeVolume: 4, DomainSpecialization: 2, IntegrationBreadth: 3, NeedWrite: true}
	got := cr.OverallComplexity()
	// 3*0.30 + 4*0.25 + 2*0.20 + 3*0.25 = 0.9 + 1.0 + 0.4 + 0.75 = 3.05
	if got < 3.04 || got > 3.06 {
		t.Errorf("OverallComplexity() = %.2f, want ~3.05", got)
	}
}

func TestComplexityResult_ThinkingIntensity(t *testing.T) {
	cr := ComplexityResult{ReasoningDepth: 5, CodeVolume: 1, DomainSpecialization: 3, IntegrationBreadth: 1}
	got := cr.ThinkingIntensity()
	// 5*0.60 + 3*0.40 = 3.0 + 1.2 = 4.2 → round to 4
	if got != 4 {
		t.Errorf("ThinkingIntensity() = %d, want 4", got)
	}
}

func TestComplexityResult_OutputIntensity(t *testing.T) {
	cr := ComplexityResult{ReasoningDepth: 1, CodeVolume: 5, DomainSpecialization: 1, IntegrationBreadth: 4}
	got := cr.OutputIntensity()
	// 5*0.50 + 4*0.30 + 1*0.20 = 2.5 + 1.2 + 0.2 = 3.9 → round to 4
	if got != 4 {
		t.Errorf("OutputIntensity() = %d, want 4", got)
	}
}

func TestClampDim(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, 1}, {1, 1}, {3, 3}, {5, 5}, {6, 5}, {-1, 1},
	}
	for _, tt := range tests {
		got := clampDim(tt.in)
		if got != tt.want {
			t.Errorf("clampDim(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTurnBasedMaxTokens(t *testing.T) {
	modelMax := 384000
	tests := []struct {
		turn     int
		expected int
	}{
		{1, 8192},
		{3, 8192},
		{4, 16384},
		{10, 16384},
		{11, 15984},
		{20, 12384},
		{40, 4384},
		{41, 4096},
		{100, 4096},
	}
	for _, tt := range tests {
		got := turnBasedMaxTokens(tt.turn, modelMax)
		if got != tt.expected {
			t.Errorf("turnBasedMaxTokens(%d, %d) = %d, want %d", tt.turn, modelMax, got, tt.expected)
		}
	}

	smallMax := 8192
	smallTests := []struct {
		turn     int
		expected int
	}{
		{1, 4096},
		{3, 4096},
		{4, 8192},
		{10, 8192},
	}
	for _, tt := range smallTests {
		got := turnBasedMaxTokens(tt.turn, smallMax)
		if got != tt.expected {
			t.Errorf("turnBasedMaxTokens(%d, %d) = %d, want %d", tt.turn, smallMax, got, tt.expected)
		}
	}
}

func TestMaxTokensController_EarlyTurnBudget(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	for turn := 1; turn <= 3; turn++ {
		budget := c.ComputeBudget(turn)
		if budget.TotalBudget != 8192 {
			t.Errorf("turn %d: expected total 8192, got %d", turn, budget.TotalBudget)
		}
		if budget.ThinkingBudget <= 0 {
			t.Errorf("turn %d: thinking budget should be positive, got %d", turn, budget.ThinkingBudget)
		}
		if budget.CompletionBudget <= 0 {
			t.Errorf("turn %d: completion budget should be positive, got %d", turn, budget.CompletionBudget)
		}
		if budget.ThinkingBudget+budget.CompletionBudget != budget.TotalBudget {
			t.Errorf("turn %d: thinking(%d) + completion(%d) != total(%d)", turn, budget.ThinkingBudget, budget.CompletionBudget, budget.TotalBudget)
		}
	}
}

func TestMaxTokensController_WithComplexity(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{
		ReasoningDepth: 4, CodeVolume: 3, DomainSpecialization: 2, IntegrationBreadth: 3, NeedWrite: true,
	})

	budget := c.ComputeBudget(5)
	if budget.TotalBudget <= 0 {
		t.Errorf("total budget should be positive, got %d", budget.TotalBudget)
	}
	if budget.ThinkingBudget <= 0 {
		t.Errorf("thinking budget should be positive, got %d", budget.ThinkingBudget)
	}
	if budget.CompletionBudget <= 0 {
		t.Errorf("completion budget should be positive, got %d", budget.CompletionBudget)
	}
}

func TestMaxTokensController_ProgressLevelCaps(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{
		ReasoningDepth: 5, CodeVolume: 5, DomainSpecialization: 5, IntegrationBreadth: 5, NeedWrite: true,
	})

	c.SetProgressLevel(ProgressLevelL1)
	budget := c.ComputeBudget(5)
	if budget.TotalBudget > 8192 {
		t.Errorf("L1 should cap total at 8192, got %d", budget.TotalBudget)
	}

	c.SetProgressLevel(ProgressLevelL2)
	budget = c.ComputeBudget(5)
	if budget.TotalBudget > 4096 {
		t.Errorf("L2 should cap total at 4096, got %d", budget.TotalBudget)
	}

	c.SetProgressLevel(ProgressLevelL3)
	budget = c.ComputeBudget(5)
	if budget.TotalBudget > 2048 {
		t.Errorf("L3 should cap total at 2048, got %d", budget.TotalBudget)
	}
}

func TestMaxTokensController_ShouldFork_OnlyOnce(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	if !c.ShouldFork() {
		t.Error("first ShouldFork should return true")
	}
	if c.ShouldFork() {
		t.Error("second ShouldFork should return false")
	}
}

func TestMaxTokensController_MaxTokensSpikeProtection(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{
		ReasoningDepth: 2, CodeVolume: 2, DomainSpecialization: 1, IntegrationBreadth: 2, NeedWrite: true,
	})

	baseline := c.ComputeEffective(5)

	// Simulate hitting max_tokens on previous turn
	c.RecordTurnOutcome("max_tokens", true, true)
	boosted := c.ComputeEffective(6)

	if boosted <= baseline {
		t.Errorf("spike protection should boost: baseline=%d, boosted=%d", baseline, boosted)
	}
}

func TestMaxTokensController_RecordTurnOutcome(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.RecordTurnOutcome("end_turn", true, false)

	c.mu.RLock()
	if c.lastStopReason != "end_turn" {
		t.Errorf("expected lastStopReason='end_turn', got %q", c.lastStopReason)
	}
	if !c.lastHadToolUse {
		t.Error("expected lastHadToolUse=true")
	}
	if c.lastHadWrite {
		t.Error("expected lastHadWrite=false")
	}
	c.mu.RUnlock()
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"complexity": 3, "is_coding": true}`, `{"complexity": 3, "is_coding": true}`},
		{`Here's my analysis: {"complexity": 3, "is_coding": true} done.`, `{"complexity": 3, "is_coding": true}`},
		{`no json here`, `no json here`},
	}

	for _, tt := range tests {
		got := extractJSON(tt.input)
		if got != tt.expected {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
