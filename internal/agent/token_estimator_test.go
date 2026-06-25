package agent

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

// TestTokenEstimator_EstimateString_NoCalibration verifies that without any
// calibration data, EstimateString produces reasonable results with the
// charset-aware base ratios.
func TestTokenEstimator_EstimateString_NoCalibration(t *testing.T) {
	estimator := NewTokenEstimator()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"english short", "hello world", 2},
		{"english medium", "The quick brown fox jumps over the lazy dog", 10},
		{"cjk short", "你好世界", 2},
		{"cjk medium", "这是一段中文测试文本用来验证估算", 10},
		{"mixed", "hello 你好 world", 4},
		{"single char", "x", 1},
		{"single cjk", "字", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimator.EstimateString(tt.input, "unknown-model")
			if got != tt.want {
				t.Errorf("EstimateString(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestTokenEstimator_EstimateString_CloseToOld verifies EstimateString is within
// 1 token of the old estimateStringTokens for common inputs. The new estimator
// uses float accumulation then truncation instead of per-component integer
// truncation, so results may differ by 1 token for mixed content.
func TestTokenEstimator_EstimateString_CloseToOld(t *testing.T) {
	estimator := NewTokenEstimator()

	testCases := []string{
		"",
		"hello",
		"你好世界",
		"mixed 中文 text",
		strings.Repeat("a", 100),
		strings.Repeat("字", 100),
	}

	for _, s := range testCases {
		oldResult := estimateStringTokens(s)
		newResult := estimator.EstimateString(s, "any-model")
		diff := newResult - oldResult
		if diff < -1 || diff > 1 {
			t.Errorf("estimate divergence for %q: old=%d new=%d (diff=%d)", s, oldResult, newResult, diff)
		}
	}
}

// TestTokenEstimator_EstimateString_WithCalibration verifies that correction
// factors are applied correctly.
func TestTokenEstimator_EstimateString_WithCalibration(t *testing.T) {
	estimator := NewTokenEstimator()

	for i := 0; i < 10; i++ {
		estimator.ObserveInput("test-model", 100, 0, 50)
	}

	corr := estimator.getInputCorrection("test-model")
	if corr < 1.5 || corr > 2.2 {
		t.Errorf("input correction should converge toward 2.0, got %f", corr)
	}

	got := estimator.EstimateString(strings.Repeat("a", 100), "test-model")
	if got < 35 || got > 55 {
		t.Errorf("corrected estimate for 100 ASCII chars should be ~44, got %d", got)
	}
}

// TestTokenEstimator_Observe_EWMA verifies the EWMA formula converges correctly.
func TestTokenEstimator_Observe_EWMA(t *testing.T) {
	estimator := NewTokenEstimator()

	// Perfect prediction: no change
	estimator.ObserveInput("m", 40, 0, 10)
	if f := estimator.getInputCorrection("m"); f != 1.0 {
		t.Errorf("after perfect prediction, factor should be 1.0, got %f", f)
	}

	// Prediction was half of actual: correction = 2.0
	estimator.ObserveInput("m", 40, 0, 20)
	f := estimator.getInputCorrection("m")
	if f < 1.15 || f > 1.25 {
		t.Errorf("after second observation, factor should be ~1.2, got %f", f)
	}
}

// TestTokenEstimator_InputVsOutputSeparation verifies input and output
// correction factors are tracked independently.
func TestTokenEstimator_InputVsOutputSeparation(t *testing.T) {
	estimator := NewTokenEstimator()

	for i := 0; i < 5; i++ {
		estimator.ObserveInput("m", 40, 0, 20)
	}
	for i := 0; i < 5; i++ {
		estimator.ObserveOutput("m", 40, 5)
	}

	inputFactor := estimator.getInputCorrection("m")
	outputFactor := estimator.getOutputCorrection("m")

	if inputFactor <= outputFactor {
		t.Errorf("input factor (%f) should be > output factor (%f) after different observations", inputFactor, outputFactor)
	}
}

// TestTokenEstimator_EstimateMessages verifies message chain estimation.
func TestTokenEstimator_EstimateMessages(t *testing.T) {
	estimator := NewTokenEstimator()

	tests := []struct {
		name     string
		messages []api.Message
		wantMin  int
		wantMax  int
	}{
		{
			name:     "empty messages",
			messages: []api.Message{},
			wantMin:  0,
			wantMax:  0,
		},
		{
			name: "single user message",
			messages: []api.Message{
				{Role: "user", Content: "hello world"},
			},
			wantMin: 1,
			wantMax: 5,
		},
		{
			name: "user and assistant with tool_use",
			messages: []api.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "Using tool", ToolUse: []api.ToolUseBlock{{ID: "1", Name: "Read", Input: map[string]any{}}}},
			},
			wantMin: 50,
			wantMax: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimator.EstimateMessages(tt.messages, "test-model")
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateMessages() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestTokenEstimator_EstimateStringOutput verifies output-specific estimation.
func TestTokenEstimator_EstimateStringOutput(t *testing.T) {
	estimator := NewTokenEstimator()

	s := "hello world"
	got := estimator.EstimateStringOutput(s, "m")
	want := estimator.EstimateString(s, "m")
	if got != want {
		t.Errorf("EstimateStringOutput without calibration should match EstimateString: got %d, want %d", got, want)
	}

	for i := 0; i < 5; i++ {
		estimator.ObserveOutput("m", 40, 5)
	}
	gotOutput := estimator.EstimateStringOutput(strings.Repeat("a", 100), "m")
	gotInput := estimator.EstimateString(strings.Repeat("a", 100), "m")
	if gotOutput >= gotInput {
		t.Errorf("output estimate (%d) should be < input estimate (%d) after output calibration", gotOutput, gotInput)
	}
}

// TestTokenEstimator_Observe_NoOp verifies edge cases for Observe.
func TestTokenEstimator_Observe_NoOp(t *testing.T) {
	estimator := NewTokenEstimator()

	estimator.ObserveInput("m", 0, 0, 10)
	if corr := estimator.getInputCorrection("m"); corr != 1.0 {
		t.Errorf("zero charCount should not create correction, got %f", corr)
	}

	estimator.ObserveInput("m", 40, 0, 0)
	if corr := estimator.getInputCorrection("m"); corr != 1.0 {
		t.Errorf("zero actualTokens should not create correction, got %f", corr)
	}

	estimator.ObserveInput("", 40, 0, 10)
	if corr := estimator.getInputCorrection(""); corr != 1.0 {
		t.Errorf("empty model should not create correction, got %f", corr)
	}
}

// TestTokenEstimator_CharsetAware verifies charset-aware estimation.
func TestTokenEstimator_CharsetAware(t *testing.T) {
	estimator := NewTokenEstimator()

	asciiEstimate := estimator.EstimateString(strings.Repeat("a", 100), "m")
	cjkEstimate := estimator.EstimateString(strings.Repeat("字", 100), "m")

	if asciiEstimate >= cjkEstimate {
		t.Errorf("ASCII estimate (%d) should be < CJK estimate (%d) for same char count", asciiEstimate, cjkEstimate)
	}
}

// TestEstimateWireTokens_IncludesOverhead verifies wire estimation exceeds message-only.
func TestEstimateWireTokens_IncludesOverhead(t *testing.T) {
	messages := []api.Message{{Role: "user", Content: "hello world"}}
	systemPrompt := []string{"You are a helpful assistant.", "Follow all instructions carefully."}
	tools := []api.ToolParam{
		{Name: "Read", Description: "Read a file", InputSchema: api.ToolInputSchema{Type: "object"}},
		{Name: "Write", Description: "Write a file", InputSchema: api.ToolInputSchema{Type: "object"}},
	}

	msgOnly := estimateTokens(messages)
	wire := estimateWireTokens(messages, systemPrompt, tools)
	if wire <= msgOnly {
		t.Errorf("wire estimate (%d) should exceed message-only (%d)", wire, msgOnly)
	}

	overhead := estimateSystemAndToolTokens(systemPrompt, tools)
	if overhead < wireFixedOverhead {
		t.Errorf("system+tool overhead (%d) should include fixed overhead (%d)", overhead, wireFixedOverhead)
	}
}

// TestEstimateWireTokens_EmptyComponents verifies zero overhead for empty system/tools.
func TestEstimateWireTokens_EmptyComponents(t *testing.T) {
	messages := []api.Message{{Role: "user", Content: "test"}}
	msgOnly := estimateTokens(messages)
	wire := estimateWireTokens(messages, nil, nil)
	want := msgOnly + wireFixedOverhead
	if wire != want {
		t.Errorf("wire with empty system/tools = %d, want %d (messages + fixed overhead)", wire, want)
	}
}

// TestTokenEstimator_EstimateWireTokens_CachesToolTokens verifies tool token cache reuse.
func TestTokenEstimator_EstimateWireTokens_CachesToolTokens(t *testing.T) {
	estimator := NewTokenEstimator()
	messages := []api.Message{{Role: "user", Content: "hello"}}
	systemPrompt := []string{"system block"}
	tools := []api.ToolParam{
		{Name: "Alpha", Description: strings.Repeat("desc ", 100), InputSchema: api.ToolInputSchema{Type: "object"}},
		{Name: "Beta", Description: strings.Repeat("desc ", 100), InputSchema: api.ToolInputSchema{Type: "object"}},
	}

	first := estimator.EstimateWireTokens(messages, systemPrompt, tools, "m")
	second := estimator.EstimateWireTokens(messages, systemPrompt, tools, "m")
	if first != second {
		t.Errorf("cached wire estimates should match: first=%d second=%d", first, second)
	}

	// Different tool set invalidates cache (different fingerprint)
	tools2 := append(tools, api.ToolParam{Name: "Gamma", Description: "extra"})
	third := estimator.EstimateWireTokens(messages, systemPrompt, tools2, "m")
	if third <= first {
		t.Errorf("adding tool should increase wire estimate: first=%d third=%d", first, third)
	}
}

// TestTokenEstimator_EstimateWireTokens_ExceedsMessages verifies calibrated wire > messages.
func TestTokenEstimator_EstimateWireTokens_ExceedsMessages(t *testing.T) {
	estimator := NewTokenEstimator()
	messages := []api.Message{{Role: "user", Content: strings.Repeat("hello ", 50)}}
	systemPrompt := []string{strings.Repeat("instruction ", 200)}
	tools := []api.ToolParam{
		{Name: "Read", Description: strings.Repeat("read files ", 50), InputSchema: api.ToolInputSchema{Type: "object"}},
	}

	msgs := estimator.EstimateMessages(messages, "m")
	wire := estimator.EstimateWireTokens(messages, systemPrompt, tools, "m")
	if wire <= msgs {
		t.Errorf("EstimateWireTokens (%d) should exceed EstimateMessages (%d)", wire, msgs)
	}
}

// TestTokenEstimator_ObserveWireInput verifies EWMA calibration against wire prediction.
func TestTokenEstimator_ObserveWireInput(t *testing.T) {
	estimator := NewTokenEstimator()
	predicted := 1000
	actual := 1100

	estimator.ObserveWireInput("m", predicted, actual)
	f := estimator.getInputCorrection("m")
	if f < 1.01 || f > 1.05 {
		t.Errorf("after single wire observation, factor should be ~1.02, got %f", f)
	}
}

// TestEstimateSystemAndToolTokens_FallbackFloor verifies MIN_SAFETY_OVERHEAD fallback helper.
func TestEffectiveWireOverhead(t *testing.T) {
	if got := effectiveWireOverhead(0); got != MIN_SAFETY_OVERHEAD {
		t.Errorf("effectiveWireOverhead(0) = %d, want MIN_SAFETY_OVERHEAD %d", got, MIN_SAFETY_OVERHEAD)
	}
	if got := effectiveWireOverhead(5000); got != 5000 {
		t.Errorf("effectiveWireOverhead(5000) = %d, want 5000", got)
	}
}

// TestToolsFingerprint_OrderIndependent verifies fingerprint ignores tool order.
func TestToolsFingerprint_OrderIndependent(t *testing.T) {
	a := toolsFingerprint([]api.ToolParam{{Name: "Alpha"}, {Name: "Beta"}})
	b := toolsFingerprint([]api.ToolParam{{Name: "Beta"}, {Name: "Alpha"}})
	if a != b {
		t.Errorf("fingerprints should match regardless of order: %q vs %q", a, b)
	}
	c := toolsFingerprint([]api.ToolParam{{Name: "Alpha"}, {Name: "Gamma"}})
	if a == c {
		t.Error("different tool sets should produce different fingerprints")
	}
}
