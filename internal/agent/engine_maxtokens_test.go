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
	// Large model (>100K maxOut) — uses higher caps
	modelMax := 384000
	tests := []struct {
		turn     int
		expected int
	}{
		{1, 8192},   // early turn: capped to prevent runaway thinking
		{3, 8192},   // still early
		{4, 16384},  // mid schedule
		{10, 16384}, // end of mid
		{11, 15984}, // taper starts: 16384 - 1*400
		{20, 12384}, // 16384 - 10*400
		{40, 4384},  // 16384 - 30*400 = 4384
		{41, 4096},  // floor
		{100, 4096}, // floor
	}

	for _, tt := range tests {
		got := turnBasedMaxTokens(tt.turn, modelMax)
		if got != tt.expected {
			t.Errorf("turnBasedMaxTokens(%d, %d) = %d, want %d",
				tt.turn, modelMax, got, tt.expected)
		}
	}

	// Small model (<100K maxOut) — uses lower caps
	smallMax := 8192
	smallTests := []struct {
		turn     int
		expected int
	}{
		{1, 4096}, // min(4096, 8192)
		{3, 4096},
		{4, 8192}, // min(16384, 8192) = 8192
		{10, 8192},
	}
	for _, tt := range smallTests {
		got := turnBasedMaxTokens(tt.turn, smallMax)
		if got != tt.expected {
			t.Errorf("turnBasedMaxTokens(%d, %d) = %d, want %d",
				tt.turn, smallMax, got, tt.expected)
		}
	}
}

func TestMaxTokensController_Discovery(t *testing.T) {
	// deepseek-chat has modelMax > 100K, so earlyTurnCap = 8192
	c := NewMaxTokensController("deepseek-chat")

	for turn := 1; turn <= 3; turn++ {
		got := c.ComputeEffective(turn)
		if got != 8192 {
			t.Errorf("turn %d: expected 8192 during discovery, got %d", turn, got)
		}
	}
}

func TestMaxTokensController_Discovery_SmallModel(t *testing.T) {
	c := &MaxTokensController{modelMax: 8192}

	for turn := 1; turn <= 3; turn++ {
		got := c.ComputeEffective(turn)
		if got != 4096 {
			t.Errorf("turn %d: expected 4096 during discovery for small model, got %d", turn, got)
		}
	}
}

func TestMaxTokensController_TurnBased_BeforeFork(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")

	got := c.ComputeEffective(5)
	if got != 16384 {
		t.Errorf("turn 5 without fork: expected 16384, got %d", got)
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
		if got != 8192 {
			t.Errorf("turn %d: discovery should use earlyTurnCap, expected 8192, got %d", turn, got)
		}
	}
}

func TestMaxTokensController_ProgressLevelCaps(t *testing.T) {
	c := NewMaxTokensController("deepseek-chat")
	c.SetComplexityResult(&ComplexityResult{Complexity: 5, IsCoding: true})

	c.SetProgressLevel(ProgressLevelL1)
	got := c.ComputeEffective(5)
	if got != 8192 {
		t.Errorf("L1 should cap at 8192, got %d", got)
	}

	c.SetProgressLevel(ProgressLevelL2)
	got = c.ComputeEffective(5)
	if got != 4096 {
		t.Errorf("L2 should cap at 4096, got %d", got)
	}
}

func TestMaxTokensController_ProgressLevel_SmallModel(t *testing.T) {
	c := &MaxTokensController{modelMax: 4096}

	c.SetProgressLevel(ProgressLevelL1)
	got := c.ComputeEffective(5)
	if got > 4096 {
		t.Errorf("L1 on small model should not exceed modelMax, got %d", got)
	}

	c.SetProgressLevel(ProgressLevelL2)
	got = c.ComputeEffective(5)
	if got > 4096 {
		t.Errorf("L2 on small model should not exceed modelMax, got %d", got)
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
