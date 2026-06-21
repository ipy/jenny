package agent

import (
	"testing"
)

func TestComputeMaxTokens_Basic(t *testing.T) {
	modelMax := 384000

	tests := []struct {
		complexity int
		isCoding   bool
		expected   int
	}{
		{1, false, 3840},
		{2, false, 7680},
		{3, false, 11520},
		{5, false, 19200},
		{1, true, 19200},
		{2, true, 38400},
		{5, true, 96000},
	}

	for _, tt := range tests {
		got := computeMaxTokens(tt.complexity, tt.isCoding, modelMax)
		if got != tt.expected {
			t.Errorf("computeMaxTokens(%d, %v, %d) = %d, want %d",
				tt.complexity, tt.isCoding, modelMax, got, tt.expected)
		}
	}
}

func TestComputeMaxTokens_MinimumFloor(t *testing.T) {
	got := computeMaxTokens(1, false, 10000)
	if got < 800 {
		t.Errorf("expected minimum 800, got %d", got)
	}
}

func TestComputeMaxTokens_CappedAtModelMax(t *testing.T) {
	got := computeMaxTokens(5, true, 10000)
	if got > 10000 {
		t.Errorf("expected capped at 10000, got %d", got)
	}
}

func TestComputeMaxTokens_ClampComplexity(t *testing.T) {
	modelMax := 384000
	got0 := computeMaxTokens(0, false, modelMax)
	got1 := computeMaxTokens(1, false, modelMax)
	if got0 != got1 {
		t.Errorf("complexity 0 should clamp to 1: got %d vs %d", got0, got1)
	}

	got6 := computeMaxTokens(6, false, modelMax)
	got5 := computeMaxTokens(5, false, modelMax)
	if got6 != got5 {
		t.Errorf("complexity 6 should clamp to 5: got %d vs %d", got6, got5)
	}
}

func TestTurnBasedMaxTokens(t *testing.T) {
	modelMax := 384000

	tests := []struct {
		turn     int
		expected int
	}{
		{1, 2000},
		{3, 2000},
		{4, 8000},
		{10, 8000},
		{11, 7800},
		{20, 6000},
		{40, 2000}, // Tapers to minimum
		{100, 2000},
	}

	for _, tt := range tests {
		got := turnBasedMaxTokens(tt.turn, modelMax)
		if got != tt.expected {
			t.Errorf("turnBasedMaxTokens(%d, %d) = %d, want %d",
				tt.turn, modelMax, got, tt.expected)
		}
	}
}

func TestMaxTokensController_Discovery(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")

	for turn := 1; turn <= 3; turn++ {
		got := c.ComputeEffective(turn)
		if got != 2000 {
			t.Errorf("turn %d: expected 2000 during discovery, got %d", turn, got)
		}
	}
}

func TestMaxTokensController_TurnBased_BeforeFork(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")

	got := c.ComputeEffective(5)
	if got != 8000 {
		t.Errorf("turn 5 without fork: expected 8000, got %d", got)
	}
}

func TestMaxTokensController_ForkOverridesTurnBased(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{Complexity: 3, IsCoding: true})

	got := c.ComputeEffective(5)
	expected := computeMaxTokens(3, true, c.modelMax)
	if got != expected {
		t.Errorf("turn 5 with fork: expected %d, got %d", expected, got)
	}
}

func TestMaxTokensController_DiscoveryIgnoresFork(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{Complexity: 5, IsCoding: true})

	for turn := 1; turn <= 3; turn++ {
		got := c.ComputeEffective(turn)
		if got != 2000 {
			t.Errorf("turn %d: discovery should ignore fork, expected 2000, got %d", turn, got)
		}
	}
}

func TestMaxTokensController_ProgressLevelCaps(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{Complexity: 5, IsCoding: true})

	c.SetProgressLevel(ProgressLevelL1)
	got := c.ComputeEffective(5)
	if got != 2000 {
		t.Errorf("L1 should cap at 2000, got %d", got)
	}

	c.SetProgressLevel(ProgressLevelL2)
	got = c.ComputeEffective(5)
	if got != 1000 {
		t.Errorf("L2 should cap at 1000, got %d", got)
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
