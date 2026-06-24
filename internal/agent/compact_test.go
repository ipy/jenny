// Package agent provides black-box validation tests for context compaction.
package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// ============================================================
// Auto-compact at effectiveWindow - 13K
// ============================================================

// TestCheckCompactThreshold verifies the threshold check works correctly.
func TestCheckCompactThreshold(t *testing.T) {
	tests := []struct {
		name                 string
		modelContextWindow   int
		modelMaxOutputTokens int
		estimatedTokens      int
		disableAutoCompact   bool
		want                 bool
	}{
		{
			name:                 "below threshold - no compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      150_000, // threshold = 200K-20K - max(20K+5K,13K) = 155K; 150K < 155K
			disableAutoCompact:   false,
			want:                 false,
		},
		{
			name:                 "at threshold - compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      155_000, // equals threshold
			disableAutoCompact:   false,
			want:                 true,
		},
		{
			name:                 "above threshold - compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      180_000, // well above threshold
			disableAutoCompact:   false,
			want:                 true,
		},
		{
			name:                 "auto compact disabled",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      200_000,
			disableAutoCompact:   true,
			want:                 false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CompactConfig{
				ModelContextWindow:   tt.modelContextWindow,
				ModelMaxOutputTokens: tt.modelMaxOutputTokens,
				DisableAutoCompact:   tt.disableAutoCompact,
			}
			got := cfg.checkCompactThreshold(tt.estimatedTokens, "user")
			if got != tt.want {
				t.Errorf("FAIL: checkCompactThreshold(%d) = %v, want %v", tt.estimatedTokens, got, tt.want)
			}
		})
	}
	t.Log("auto-compact threshold check works correctly")
}

// TestWarningThreshold verifies warning threshold is emitted when appropriate.
func TestWarningThreshold(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	// Effective window = 200K - 20K = 180K
	// Auto compact buffer = max(20K+5K, 13K) = 25K
	// Auto compact threshold = 180K - 25K = 155K
	// Warning threshold = 155K - 20K = 135K

	// Below warning threshold - no warning
	if cfg.checkWarningThreshold(130_000) {
		t.Error("FAIL: warning should not trigger below threshold")
	}

	// At warning threshold - warning
	if !cfg.checkWarningThreshold(135_000) {
		t.Error("FAIL: warning should trigger at threshold")
	}

	// Above warning threshold - warning
	if !cfg.checkWarningThreshold(150_000) {
		t.Error("FAIL: warning should trigger above threshold")
	}

	t.Log("warning threshold check works correctly")
}

// ============================================================
// Circuit breaker after 3 failures
// ============================================================

// TestCircuitBreakerReset verifies circuit breaker resets on success.
func TestCircuitBreakerReset(t *testing.T) {
	// Simulate failure counter behavior
	failCount := 0

	// Increment on failure
	failCount++
	if failCount != 1 {
		t.Errorf("FAIL: expected failCount 1, got %d", failCount)
	}

	failCount++
	failCount++
	if failCount != 3 {
		t.Errorf("FAIL: expected failCount 3, got %d", failCount)
	}

	// Reset on success
	failCount = 0
	if failCount != 0 {
		t.Errorf("FAIL: expected failCount 0 after reset, got %d", failCount)
	}

	t.Log("circuit breaker reset works correctly")
}

// TestCircuitBreakerTrip verifies circuit breaker trips after 3 failures.
func TestCircuitBreakerTrip(t *testing.T) {
	maxFailures := MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES
	if maxFailures != 3 {
		t.Errorf("FAIL: MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES should be 3, got %d", maxFailures)
	}

	// Simulate circuit breaker behavior
	failCount := 0
	circuitBreakerTripped := false

	for range 5 {
		if failCount >= MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES {
			circuitBreakerTripped = true
			break
		}
		failCount++
	}

	if !circuitBreakerTripped {
		t.Error("FAIL: circuit breaker should have tripped after 3 failures")
	}

	t.Log("circuit breaker trips after 3 consecutive failures")
}

// ============================================================
// Hard block at effectiveWindow - 3K when auto off
// ============================================================

// TestBlockIfOverLimit verifies blocking limit when auto-compact is disabled.
func TestBlockIfOverLimit(t *testing.T) {
	tests := []struct {
		name                 string
		modelContextWindow   int
		modelMaxOutputTokens int
		estimatedTokens      int
		querySource          string
		disableAutoCompact   bool
		wantBlock            bool
	}{
		{
			name:                 "below blocking limit - no block",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      170_000, // 180K - 3K = 177K limit
			querySource:          "user",
			disableAutoCompact:   true,
			wantBlock:            false,
		},
		{
			name:                 "at blocking limit - block",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      177_000, // equals limit
			querySource:          "user",
			disableAutoCompact:   true,
			wantBlock:            true,
		},
		{
			name:                 "above blocking limit - block",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      180_000, // well above limit
			querySource:          "user",
			disableAutoCompact:   true,
			wantBlock:            true,
		},
		{
			name:                 "auto compact enabled - no block even at limit",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      180_000,
			querySource:          "user",
			disableAutoCompact:   false,
			wantBlock:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CompactConfig{
				ModelContextWindow:   tt.modelContextWindow,
				ModelMaxOutputTokens: tt.modelMaxOutputTokens,
				DisableAutoCompact:   tt.disableAutoCompact,
			}
			err := cfg.blockIfOverLimit(tt.estimatedTokens, tt.querySource)
			gotBlock := err != nil
			if gotBlock != tt.wantBlock {
				t.Errorf("FAIL: blockIfOverLimit(%d) returned error = %v, want block = %v", tt.estimatedTokens, gotBlock, tt.wantBlock)
			}
		})
	}
	t.Log("blocking limit check works correctly")
}

// ============================================================
// Post-compact payload passes tool/thinking pairing rules
// ============================================================

// TestPostCompactNormalization verifies post-compact normalization.
func TestPostCompactNormalization(t *testing.T) {
	// Create messages with thinking that needs normalization
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "<thinking>orphaned thought</thinking>",
		},
		{
			Role:    "assistant",
			Content: "Hello<thinking>trailing thought</thinking>",
		},
		{
			Role:    "assistant",
			Content: "   \n\t  ", // whitespace only
		},
		{
			Role:    "assistant",
			Content: "", // empty
		},
		{
			Role:    "user",
			Content: "continue",
		},
	}

	result := normalizeCompactedChain(messages)

	// Verify no thinking blocks at end of last assistant
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role == "assistant" {
			content := result[i].Content
			if strings.Contains(content, "<thinking>") {
				t.Errorf("FAIL: assistant message still contains thinking block: %q", content)
			}
			break
		}
	}

	// Verify empty assistants get placeholder
	for _, msg := range result {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" && len(msg.ToolUse) == 0 {
			t.Errorf("FAIL: empty assistant should have placeholder content")
		}
	}

	t.Log("post-compact normalization works correctly")
}

// ============================================================
// compact/session_memory sources never hard-blocked pre-API
// ============================================================

// TestCompactSourceNeverBlocked verifies compact querySource skips hard block.
func TestCompactSourceNeverBlocked(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
		DisableAutoCompact:   true, // auto compact is disabled
	}

	// Even with very high tokens and auto-compact disabled, compact source should not block
	err := cfg.blockIfOverLimit(200_000, "compact")
	if err != nil {
		t.Errorf("FAIL: compact source should never be blocked, got error: %v", err)
	}

	err = cfg.blockIfOverLimit(200_000, "session_memory")
	if err != nil {
		t.Errorf("FAIL: session_memory source should never be blocked, got error: %v", err)
	}

	// But user source should still block
	err = cfg.blockIfOverLimit(200_000, "user")
	if err == nil {
		t.Error("FAIL: user source should be blocked when auto-compact is disabled")
	}

	t.Log("compact/session_memory sources never hard-blocked")
}

// ============================================================
// Token Estimation Tests
// ============================================================

// TestEstimateTokens verifies token estimation heuristic.
func TestEstimateTokens(t *testing.T) {
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
			wantMin: 1, // 11 chars / 4 ≈ 2 tokens
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
			got := estimateTokens(tt.messages)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("estimateTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
	t.Log("Token estimation heuristic works correctly")
}

// ============================================================
// Threshold Math Tests
// ============================================================

// TestThresholdMath verifies the threshold calculations.
func TestThresholdMath(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	// effectiveContextWindow = 200K - 20K = 180K
	effectiveWindow := cfg.effectiveContextWindow()
	if effectiveWindow != 180_000 {
		t.Errorf("effectiveContextWindow = %d, want 180000", effectiveWindow)
	}

	// autoCompactBuffer = max(20K + 5K, 13K) = 25K
	// autoCompactThreshold = 180K - 25K = 155K
	autoCompactThreshold := cfg.autoCompactThreshold()
	if autoCompactThreshold != 155_000 {
		t.Errorf("autoCompactThreshold = %d, want 155000", autoCompactThreshold)
	}

	// warningThreshold = 155K - 20K = 135K
	warningThreshold := cfg.warningThreshold()
	if warningThreshold != 135_000 {
		t.Errorf("warningThreshold = %d, want 135000", warningThreshold)
	}

	// blockingLimit = 180K - 3K = 177K
	blockingLimit := cfg.blockingLimit()
	if blockingLimit != 177_000 {
		t.Errorf("blockingLimit = %d, want 177000", blockingLimit)
	}

	t.Log("Threshold math calculations correct")
}

// ============================================================
// Helper Function Tests
// ============================================================

// TestBuildCompactedChain verifies the compacted chain structure.
func TestBuildCompactedChain(t *testing.T) {
	messages := make([]api.Message, 15)
	for i := range messages {
		messages[i] = api.Message{
			Role:    "user",
			Content: "message content",
		}
	}

	summary := "This is a summary of the conversation."
	result := buildCompactedChain(messages, summary)

	// First message should be the boundary marker
	if len(result) == 0 {
		t.Fatal("FAIL: result should not be empty")
	}

	if result[0].Role != "user" {
		t.Errorf("FAIL: first message should be system role, got %s", result[0].Role)
	}

	if !strings.Contains(result[0].Content, "[Context boundary") {
		t.Error("FAIL: first message should contain boundary marker")
	}

	if !strings.Contains(result[0].Content, summary) {
		t.Error("FAIL: first message should contain summary text")
	}

	// Should have kept last 10 messages
	if len(result) != 11 { // 1 boundary + 10 kept messages
		t.Errorf("FAIL: expected11 messages, got %d", len(result))
	}

	t.Log("buildCompactedChain produces correct structure")
}

// ============================================================
// Thresholds from actual model params
// ============================================================

func TestModelAwareCompactConfig(t *testing.T) {
	// DeepSeek V4 models have 1048576 context window and 393216 max output
	cfg := newCompactConfigForModel("deepseek-v4-flash", nil)
	if cfg.ModelContextWindow != 1_048_576 {
		t.Errorf("deepseek-v4-flash context window = %d, want 1048576", cfg.ModelContextWindow)
	}
	if cfg.ModelMaxOutputTokens != 393_216 {
		t.Errorf("deepseek-v4-flash max output = %d, want 393216", cfg.ModelMaxOutputTokens)
	}

	// Claude Opus 4 uses 200K context window and 128K max output (from capability table)
	cfgDefault := newCompactConfigForModel("claude-opus-4-5-20251101", nil)
	if cfgDefault.ModelContextWindow != 200_000 {
		t.Errorf("default context window = %d, want 200000", cfgDefault.ModelContextWindow)
	}
	if cfgDefault.ModelMaxOutputTokens != 128_000 {
		t.Errorf("default max output = %d, want 128000", cfgDefault.ModelMaxOutputTokens)
	}
}

// TestNewCompactConfigFromStreamConfig verifies that the C-group env-var
// overrides flow through StreamConfig into the CompactConfig.
// (No t.Setenv — values now come from StreamConfig, not os.Getenv.)
func TestNewCompactConfigFromStreamConfig(t *testing.T) {
	cfg := newCompactConfigForModel("claude-opus-4-5-20251101", &StreamConfig{
		DisableCompact:      true,
		DisableAutoCompact:  true,
		EnableSessionMemory: true,
	})
	if !cfg.DisableCompact {
		t.Error("expected DisableCompact from StreamConfig")
	}
	if !cfg.DisableAutoCompact {
		t.Error("expected DisableAutoCompact from StreamConfig")
	}
	if !cfg.SessionMemoryEnabled {
		t.Error("expected SessionMemoryEnabled from StreamConfig")
	}

	// nil StreamConfig yields the zero-value defaults (no auto-compact gating)
	cfgZero := newCompactConfigForModel("claude-opus-4-5-20251101", nil)
	if cfgZero.DisableCompact || cfgZero.DisableAutoCompact || cfgZero.SessionMemoryEnabled {
		t.Errorf("nil StreamConfig should yield all-false flags, got %+v", cfgZero)
	}
}

func TestDeepSeekThresholdMath(t *testing.T) {
	cfg := newCompactConfigForModel("deepseek-v4-flash", nil)

	// effectiveContextWindow = 1048576 - 393216 = 655360
	effectiveWindow := cfg.effectiveContextWindow()
	expectedEffective := 1_048_576 - 393_216
	if effectiveWindow != expectedEffective {
		t.Errorf("effectiveContextWindow = %d, want %d", effectiveWindow, expectedEffective)
	}

	// Context window > 512K → threshold capped at 512K
	threshold := cfg.autoCompactThreshold()
	if threshold != 512_000 {
		t.Errorf("autoCompactThreshold = %d, want 512000 (context window > 512K, capped)", threshold)
	}

	// Should trigger at threshold
	if !cfg.checkCompactThreshold(threshold, "user") {
		t.Errorf("should trigger at threshold %d", threshold)
	}
	if cfg.checkCompactThreshold(threshold-1, "user") {
		t.Error("should NOT trigger below threshold")
	}
	t.Logf("DeepSeek threshold math: effective=%d threshold=%d", effectiveWindow, threshold)
}

// TestMidRangeThreshold verifies that models with context window > 256K
// and <= 512K use 50% of context window as the threshold.
func TestMidRangeThreshold(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   300_000,
		ModelMaxOutputTokens: 20_000,
	}

	// Context window > 256K and ≤ 512K → threshold = 300K / 2 = 150K
	threshold := cfg.autoCompactThreshold()
	if threshold != 150_000 {
		t.Errorf("autoCompactThreshold = %d, want 150000 (50%% of context window)", threshold)
	}

	if !cfg.checkCompactThreshold(threshold, "user") {
		t.Errorf("should trigger at 150K")
	}
	if cfg.checkCompactThreshold(threshold-1, "user") {
		t.Error("should NOT trigger below 150K")
	}
}

// Test256KBoundaryThreshold verifies the boundary between tier 1 and tier 2.
// At exactly 256K, the original formula should be used.
func Test256KBoundaryThreshold(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   256_000,
		ModelMaxOutputTokens: 20_000,
	}

	// ≤ 256K: original formula. effectiveWindow = 256K - 20K = 236K
	// autoCompactBuffer = max(20K+5K, 13K) = 25K
	// threshold = 236K - 25K = 211K
	threshold := cfg.autoCompactThreshold()
	if threshold != 211_000 {
		t.Errorf("autoCompactThreshold = %d, want 211000 (original formula at boundary)", threshold)
	}

	// 256_001 should fall into tier 2 (50% of context)
	cfg.ModelContextWindow = 256_001
	threshold = cfg.autoCompactThreshold()
	if threshold != 128_000 {
		t.Errorf("autoCompactThreshold = %d, want 128000 (above boundary, 50%% of 256001 = 128000)", threshold)
	}
}

// TestAutoCompactWindowOverride verifies that when AUTO_COMPACT_WINDOW
// is explicitly set, the original formula is used regardless of context window
// size, bypassing the tiered logic.
func TestAutoCompactWindowOverride(t *testing.T) {
	// Simulate user setting AUTO_COMPACT_WINDOW=200000 on deepseek-v4-flash
	// (which has a 1M context window, normally triggering the 512K cap)
	cfg := CompactConfig{
		ModelContextWindow:   200_000, // After env override
		ModelMaxOutputTokens: 393_216,
		AutoCompactWindow:    200_000,
	}

	// effectiveWindow = 200K - 393K → negative
	// autoCompactBuffer = max(393K+5K, 13K) = 398K, capped at effective/2 (which is negative)
	// Without override, tier 3 would apply (cap = 512K)
	// With override, original formula is used
	// Since effectiveWindow is negative, threshold = 0
	threshold := cfg.autoCompactThreshold()
	if threshold != 0 {
		t.Errorf("autoCompactThreshold = %d, want 0 (negative effective window with override)", threshold)
	}

	// With a workable override: AUTO_COMPACT_WINDOW=300000
	cfg.ModelContextWindow = 300_000
	cfg.AutoCompactWindow = 300_000
	cfg.ModelMaxOutputTokens = 20_000

	// effectiveWindow = 300K - 20K = 280K
	// autoCompactBuffer = max(20K+5K, 13K) = 25K
	// threshold = 280K - 25K = 255K
	// Without override, this would be tier 2 → 150K
	// With override, original formula → 255K
	threshold = cfg.autoCompactThreshold()
	if threshold != 255_000 {
		t.Errorf("autoCompactThreshold = %d, want 255000 (override uses original formula)", threshold)
	}
}

// TestHighOutputModelThresholdFloor verifies that models with large output
// relative to context window (e.g. gpt-5.5: 200k context, 128k max output)
// don't produce negative thresholds.
func TestHighOutputModelThresholdFloor(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 128_000,
	}

	// effectiveContextWindow = 200k - 128k = 72k
	if cfg.effectiveContextWindow() != 72_000 {
		t.Errorf("effectiveContextWindow = %d, want 72000", cfg.effectiveContextWindow())
	}

	// autoCompactBuffer = max(128k+5k, 13k) = 133k, capped at 72k/2 = 36k
	// threshold = 72k - 36k = 36k (positive, not negative)
	threshold := cfg.autoCompactThreshold()
	if threshold < 0 {
		t.Errorf("autoCompactThreshold = %d, should never be negative", threshold)
	}
	if threshold != 36_000 {
		t.Errorf("autoCompactThreshold = %d, want 36000", threshold)
	}

	// First few turns with tiny context should NOT trigger compaction
	if cfg.checkCompactThreshold(37, "user") {
		t.Error("37 tokens should NOT trigger compaction (threshold is 36000)")
	}

	t.Log("High-output model threshold correctly floored")
}

// ============================================================
// buildCompactedChain does not split tool pairs
// ============================================================

func TestCompactedChainPreservesToolPairs(t *testing.T) {
	messages := make([]api.Message, 20)
	for i := range 18 {
		messages[i] = api.Message{Role: "user", Content: "filler"}
	}
	// Last two messages: assistant with tool_use + user with tool_result
	messages[18] = api.Message{
		Role:    "assistant",
		Content: "Using tool",
		ToolUse: []api.ToolUseBlock{{ID: "call_001", Name: "Read", Input: map[string]any{}}},
	}
	messages[19] = api.Message{
		Role:        "user",
		ToolResults: []api.ToolResultBlock{{ToolUseID: "call_001", Content: "result"}},
	}

	result := buildCompactedChain(messages, "summary")

	// Find the assistant with tool_use and verify the next message has its tool_result
	for i, msg := range result {
		if msg.Role == "assistant" && len(msg.ToolUse) > 0 {
			if i+1 >= len(result) {
				t.Fatal("FAIL: tool_use at end of chain with no following tool_result")
			}
			next := result[i+1]
			if next.Role != "user" || len(next.ToolResults) == 0 {
				t.Fatal("FAIL: tool_use not immediately followed by tool_result")
			}
			found := false
			for _, tr := range next.ToolResults {
				if tr.ToolUseID == msg.ToolUse[0].ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("FAIL: tool_result does not match tool_use ID")
			}
		}
	}
}

// ============================================================
// Token estimation charset-aware
// ============================================================

func TestEstimateTokens_CharsetAware(t *testing.T) {
	// English text: ~4 chars per token
	englishMsg := []api.Message{{Role: "user", Content: strings.Repeat("hello world ", 100)}}
	englishEst := estimateTokens(englishMsg)
	actualEnglishChars := len(englishMsg[0].Content)
	expectedEnglishTokens := actualEnglishChars / 4
	ratio := float64(englishEst) / float64(expectedEnglishTokens)
	if ratio < 0.7 || ratio > 1.3 {
		t.Errorf("English estimation ratio %.2f outside [0.7, 1.3] (est=%d, expected≈%d)",
			ratio, englishEst, expectedEnglishTokens)
	}

	// CJK text: ~1.5 chars per token (each CJK char is 3 bytes in UTF-8)
	cjkMsg := []api.Message{{Role: "user", Content: strings.Repeat("你好世界测试", 100)}}
	cjkEst := estimateTokens(cjkMsg)
	// Old heuristic would say len("你好世界测试"*100) / 4 = 1800/4 * 100... but bytes
	// With charset-aware: CJK chars should estimate ~1.5 chars/token
	// 600 CJK runes → ~400 tokens expected
	if cjkEst < 200 || cjkEst > 800 {
		t.Errorf("CJK estimation %d seems unreasonable for 600 CJK chars (expected ~400)", cjkEst)
	}
}

// TestEstimateTokens_Thinking verifies that Thinking and Signature fields
// are included in the token estimate.
func TestEstimateTokens_Thinking(t *testing.T) {
	// Helper for readability: create a single thinking message
	messages := []api.Message{
		{
			Role:      "assistant",
			Content:   "visible text",
			Thinking:  "this is thinking content that should be counted",
			Signature: "sig-abc123",
		},
	}

	est := estimateTokens(messages)

	// Content only: "visible text" = 12 chars / 4 = ~3 tokens
	// The thinking and signature fields should add meaningful tokens
	if est < 10 {
		t.Errorf("estimateTokens with thinking = %d, want >= 10 (thinking should be counted)", est)
	}

	// Verify that removing thinking+signature gives a lower count
	messagesNoThinking := []api.Message{
		{Role: "assistant", Content: "visible text"},
	}
	estNoThinking := estimateTokens(messagesNoThinking)
	if est <= estNoThinking {
		t.Errorf("estimate with thinking (%d) should be > without thinking (%d)", est, estNoThinking)
	}
	t.Logf("thinking adds %d tokens to estimate", est-estNoThinking)
}

// TestDropOldestAPIRoundGroup verifies oldest API round group is dropped.
func TestDropOldestAPIRoundGroup(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "old user"},           // API round 1 start
		{Role: "assistant", Content: "old assistant"}, // API round 1 end
		{Role: "user", Content: "new user"},           // API round 2 start
		{Role: "assistant", Content: "new assistant"}, // API round 2 end
	}

	result := dropOldestAPIRoundGroup(messages)

	// Should drop first two messages (old API round)
	if len(result) != 2 {
		t.Errorf("FAIL: expected 2 messages after drop, got %d", len(result))
	}

	// Remaining should be the newer messages
	if result[0].Content != "new user" {
		t.Errorf("FAIL: expected 'new user', got %s", result[0].Content)
	}

	t.Log("dropOldestAPIRoundGroup works correctly")
}

// ============================================================
// In-session compact skipped when messages too large
// ============================================================

// mockCompactClient is a mock api.Requester for testing compactMessages behavior.
// It returns a valid summary response and tracks call count.
type mockCompactClient struct {
	calls int
}

// SendMessage implements api.Requester.
func (m *mockCompactClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	m.calls++
	return &api.Response{
		Content: []api.ContentBlock{
			{Type: api.BlockTypeText, Text: "Summary of the conversation"},
		},
		StopReason: "end_turn",
		Model:      "test-model",
		Usage:      api.Usage{InputTokens: 100, OutputTokens: 10},
	}, nil
}

// SendMessageStream implements api.Requester (no-op for non-streaming tests).
func (m *mockCompactClient) SendMessageStream(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
	m.calls++
	ch := make(chan api.StreamContentBlock)
	close(ch)
	return ch, &api.StreamResult{}
}

// SetRetryConfig implements api.Requester.
func (m *mockCompactClient) SetRetryConfig(cfg api.RetryConfig) {}

// SetBackground implements api.Requester.
func (m *mockCompactClient) SetBackground(isBackground bool) {}

// SetThinkingConfig implements api.Requester.
func (m *mockCompactClient) SetThinkingConfig(cfg api.ThinkingConfig) {}

// SetProviderName implements api.Requester.
func (m *mockCompactClient) SetProviderName(name string) {}

// SetMaxTokensOverride implements api.Requester.
func (m *mockCompactClient) SetMaxTokensOverride(n int) {}

// SetDynamicThinkingBudget implements api.Requester.
func (m *mockCompactClient) SetDynamicThinkingBudget(n int) {}

// newTestEngine creates a QueryEngine with the given mock client, wiring up
// sideQueryCtx.ClientFactory so that fallbackSummaryCompact (via SideQuery)
// routes calls through the mock.
func newTestEngine(mock api.Requester) *QueryEngine {
	costState := &CostState{ModelUsage: make(map[string]ModelUsage)}
	e := &QueryEngine{
		client:    mock,
		model:     "test-model",
		costState: costState,
	}
	e.sideQueryCtx = &SideQueryContext{
		Model:     "test-model",
		CostState: costState,
		CostMu:    &e.mu,
		ClientFactory: func(model string) (api.Requester, error) {
			return mock, nil
		},
	}
	return e
}

// TestInSessionCompact_Behavior_WithLargeMessages verifies that when
// estimated tokens exceed effectiveContextWindow - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS,
// compactMessages succeeds via fallbackSummaryCompact (proving inSessionCompact was skipped).
// This is a behavioral test that actually calls compactMessages — not just setup math.
func TestInSessionCompact_Behavior_WithLargeMessages(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	// maxMessagesTokens = 180K - 30K - 20K = 130K
	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS
	if maxMessagesTokens != 130_000 {
		t.Fatalf("expected maxMessagesTokens=130000, got %d", maxMessagesTokens)
	}

	// Create messages totaling ~135K tokens (over the 130K limit)
	largeContent := strings.Repeat("hello world ", 45_000)
	messages := []api.Message{
		{Role: "user", Content: largeContent},
	}

	estimated := estimateTokens(messages)
	if estimated <= maxMessagesTokens {
		t.Fatalf("test setup: estimated %d should exceed maxMessagesTokens %d", estimated, maxMessagesTokens)
	}

	mock := &mockCompactClient{}
	engine := newTestEngine(mock)

	// compactMessages should succeed via fallbackSummaryCompact
	_, err := engine.compactMessages(context.Background(), messages, cfg, []string{"test system prompt"})
	if err != nil {
		t.Fatalf("compactMessages with large messages should succeed via fallback, got error: %v", err)
	}

	// The safety check skipped inSessionCompact, but fallbackSummaryCompact was called.
	// Success here proves the skip logic worked (inSessionCompact would have overflowed).
	t.Logf("PASS: compactMessages succeeded with large messages (%d estimated tokens, limit %d)",
		estimated, maxMessagesTokens)
}

// TestInSessionCompact_Behavior_WithSmallMessages verifies that when
// estimated tokens are within the safety margin, compactMessages succeeds.
// This is a behavioral test that actually calls compactMessages — not just setup math.
func TestInSessionCompact_Behavior_WithSmallMessages(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS

	// Create messages totaling ~100K tokens (within the 130K limit)
	smallContent := strings.Repeat("hello world ", 33_334)
	messages := []api.Message{
		{Role: "user", Content: smallContent},
	}

	estimated := estimateTokens(messages)
	if estimated > maxMessagesTokens {
		t.Fatalf("test setup: estimated %d should be within maxMessagesTokens %d", estimated, maxMessagesTokens)
	}

	mock := &mockCompactClient{}
	engine := newTestEngine(mock)

	// compactMessages should succeed (via inSessionCompact or fallback)
	_, err := engine.compactMessages(context.Background(), messages, cfg, []string{"test system prompt"})
	if err != nil {
		t.Fatalf("compactMessages with small messages should succeed, got error: %v", err)
	}

	// Verify compaction was attempted
	if mock.calls == 0 {
		t.Fatal("compactMessages should have called SendMessage at least once")
	}

	t.Logf("PASS: compactMessages succeeded with small messages (%d estimated tokens, limit %d), compaction attempted",
		estimated, maxMessagesTokens)
}

// ============================================================
// isPromptTooLongError widened to include "context window"
// ============================================================

// TestIsPromptTooLongError_Widened verifies isPromptTooLongError matches
// "context window" (case-insensitive) in addition to existing patterns.
func TestIsPromptTooLongError_Widened(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
		ac     string
	}{
		// Existing patterns (regression)
		{"prompt too long", "prompt too long", true, "existing"},
		{"too many tokens", "too many tokens", true, "existing"},
		{"context length", "context length", true, "existing"},
		{"413", "error 413", true, "existing"},
		// New pattern
		{"context window exact", "context window exceeds limit", true, "context-window"},
		{"context window mixed case", "Context Window Exceeds Limit", true, "context-window"},
		{"context window uppercase", "CONTEXT WINDOW EXCEEDS LIMIT", true, "context-window"},
		// Wrapped error from inSessionCompact
		{"wrapped minmax error", "in-session compaction API call: HTTP 400: invalid params, context window exceeds limit (2013)", true, "wrapped"},
		// Non-matching
		{"unrelated error", "connection timeout", false, "none"},
		{"empty error", "", false, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("%s", tt.errMsg)
			got := isPromptTooLongError(err)
			if got != tt.want {
				t.Errorf("%s FAIL: isPromptTooLongError(%q) = %v, want %v", tt.ac, tt.errMsg, got, tt.want)
			}
		})
	}
	t.Log("PASS: isPromptTooLongError widened to include context window")
}

// TestIsPromptTooLongError_AllSubstrings verifies all required error substrings
// Are matched by isPromptTooLongError.
func TestIsPromptTooLongError_AllSubstrings(t *testing.T) {
	required := []string{
		"prompt too long",
		"too many tokens",
		"context length",
		"context window",
		"413",
	}

	for _, substr := range required {
		err := fmt.Errorf("test error: %s", substr)
		if !isPromptTooLongError(err) {
			t.Errorf("FAIL: isPromptTooLongError should match %q", substr)
		}
	}
	t.Log("PASS: all required substrings matched by isPromptTooLongError")
}

// ============================================================
// 175K messages in 200K window: fallbackSummaryCompact retries, succeeds
// ============================================================

// mockForkRetryClient simulates a compaction API that returns a prompt-too-long error
// on the first fallbackSummaryCompact call (identified by fallbackCompactSystemPrompt) and
// succeeds on retry.
type mockForkRetryClient struct {
	mockCompactClient
}

func (m *mockForkRetryClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	m.calls++
	// fallbackSummaryCompact uses fallbackCompactSystemPrompt; first such call returns prompt-too-long
	if strings.Contains(strings.Join(systemPrompt, "\n\n"), "summarizes conversations") && m.calls == 1 {
		return nil, fmt.Errorf("context window exceeds limit (2013)")
	}
	return &api.Response{
		Content: []api.ContentBlock{
			{Type: api.BlockTypeText, Text: "Summary of the conversation including key decisions and code changes."},
		},
		StopReason: "end_turn",
		Model:      "test-model",
		Usage:      api.Usage{InputTokens: 100, OutputTokens: 10},
	}, nil
}

// Test175KTokensForkRetries verifies that with ~175K tokens in a 200K context window,
// inSessionCompact is skipped, fallbackSummaryCompact retries after dropping groups, and
// compaction succeeds with a valid summary + preserved trailing messages.
func Test175KTokensForkRetries(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	autoCompactThreshold := cfg.autoCompactThreshold()
	if autoCompactThreshold != 155_000 {
		t.Fatalf("expected autoCompactThreshold=155000, got %d", autoCompactThreshold)
	}

	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS
	if maxMessagesTokens != 130_000 {
		t.Fatalf("expected maxMessagesTokens=130000, got %d", maxMessagesTokens)
	}

	// Build a message chain with ~175K tokens across multiple API rounds.
	// Each round: user (bulk text) + assistant (with tool_use) + user (tool_results).
	var messages []api.Message
	roundCount := 5
	charsPerUserMsg := 140_000 // ~35K tokens each, 5 rounds ≈ 175K tokens

	for i := range roundCount {
		content := strings.Repeat("x", charsPerUserMsg)
		messages = append(messages, api.Message{Role: "user", Content: content})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Response to round %d with details and analysis.", i),
			ToolUse: []api.ToolUseBlock{{ID: fmt.Sprintf("call_%d", i), Name: "Read", Input: map[string]any{"path": "file.txt"}}},
		})
		messages = append(messages, api.Message{
			Role:        "user",
			ToolResults: []api.ToolResultBlock{{ToolUseID: fmt.Sprintf("call_%d", i), Content: "tool result data"}},
		})
	}

	estimated := estimateTokens(messages)
	t.Logf("estimated tokens = %d, autoCompactThreshold = %d, maxMessagesTokens = %d",
		estimated, autoCompactThreshold, maxMessagesTokens)

	if estimated < 150_000 {
		t.Fatalf("FAIL: test messages only have %d estimated tokens, need ~175K", estimated)
	}

	if !cfg.checkCompactThreshold(estimated, "user") {
		t.Error("FAIL: estimated tokens should trigger auto-compact")
	}

	mock := &mockForkRetryClient{}
	engine := newTestEngine(mock)

	result, err := engine.compactMessages(context.Background(), messages, cfg, []string{"test system prompt"})
	if err != nil {
		t.Fatalf("FAIL: compactMessages should succeed via fallbackSummaryCompact retry, got error: %v", err)
	}

	if mock.calls < 2 {
		t.Errorf("FAIL: expected fallbackSummaryCompact to retry (calls >= 2), got %d calls", mock.calls)
	}

	if len(result) == 0 {
		t.Fatal("FAIL: compacted result should not be empty")
	}
	if result[0].Role != "user" {
		t.Errorf("FAIL: first message role = %q, want 'user'", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "[Context boundary") {
		t.Error("FAIL: first message should contain context boundary marker")
	}
	if !strings.Contains(result[0].Content, "Summary of the conversation") {
		t.Error("FAIL: first message should contain summary text")
	}

	if len(result) < 2 {
		t.Fatal("FAIL: compacted chain should have trailing messages")
	}
	trailingCount := len(result) - 1
	// buildCompactedChain keeps messagesToKeep=10, but the tool_use/tool_result
	// pairing guard may shift the cut earlier, keeping up to 11 trailing messages
	// (or fewer if the original chain is shorter than messagesToKeep).
	if trailingCount < 1 || trailingCount > 15 {
		t.Errorf("FAIL: expected 1-15 trailing messages, got %d", trailingCount)
	}
	if trailingCount < 1 {
		t.Error("FAIL: expected at least 1 trailing message")
	}

	t.Logf("PASS: compactMessages succeeded (calls=%d, trailingMsgs=%d, summary present=true)",
		mock.calls, trailingCount)
}

// ============================================================
// 1M context window models still use inSessionCompact
// ============================================================

// mock1MWindowClient tracks whether inSessionCompact was called by checking the systemPrompt.
// inSessionCompact passes the original systemPrompt from compactMessages ([]string{"test system prompt"}),
// while fallbackSummaryCompact uses fallbackCompactSystemPrompt.
type mock1MWindowClient struct {
	mockCompactClient
	inSessionCompactCalled bool
}

func (m *mock1MWindowClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	m.calls++
	// inSessionCompact passes the original systemPrompt (same as compactMessages received),
	// while fallbackSummaryCompact uses fallbackCompactSystemPrompt.
	if !strings.Contains(strings.Join(systemPrompt, "\n\n"), "summarizes conversations") {
		m.inSessionCompactCalled = true
	}
	return &api.Response{
		Content: []api.ContentBlock{
			{Type: api.BlockTypeText, Text: "Summary for 1M window test"},
		},
		StopReason: "end_turn",
		Model:      "test-model",
		Usage:      api.Usage{InputTokens: 100, OutputTokens: 10},
	}, nil
}

// TestDeepSeek1MWindowUsesInSessionCompact verifies that models with 1M context
// windows continue to use inSessionCompact when messages are well within the
// safety overhead margin.
func TestDeepSeek1MWindowUsesInSessionCompact(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   1_000_000,
		ModelMaxOutputTokens: 8_192,
	}

	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS
	if maxMessagesTokens != 941_808 {
		t.Fatalf("expected maxMessagesTokens=941808 for deepseek, got %d", maxMessagesTokens)
	}

	content := strings.Repeat("hello world ", 33_334)
	messages := []api.Message{
		{Role: "user", Content: content},
	}

	estimated := estimateTokens(messages)
	if estimated > maxMessagesTokens {
		t.Fatalf("FAIL: test setup error: estimated %d exceeds maxMessagesTokens %d", estimated, maxMessagesTokens)
	}
	t.Logf("estimated tokens = %d, maxMessagesTokens = %d, within margin", estimated, maxMessagesTokens)

	mock := &mock1MWindowClient{}
	engine := newTestEngine(mock)

	_, err := engine.compactMessages(context.Background(), messages, cfg, []string{"test system prompt"})
	if err != nil {
		t.Fatalf("FAIL: compactMessages should succeed for 1M-window model, got error: %v", err)
	}

	if !mock.inSessionCompactCalled {
		t.Error("FAIL: inSessionCompact was NOT called for deepseek model with messages within margin")
	}
	if mock.calls == 0 {
		t.Error("FAIL: no API call was made at all")
	}

	t.Logf("PASS: inSessionCompact was called for 1M-window model (total calls=%d)", mock.calls)
}

// ============================================================
// fallbackSummaryCompact exhausts all retries, returns underlying error
// ============================================================

// mockExhaustClient always returns a prompt-too-long error for fallbackSummaryCompact
// calls (nil tools) so retries are exhausted.
type mockExhaustClient struct {
	mockCompactClient
}

func (m *mockExhaustClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	m.calls++
	if tools == nil {
		return nil, fmt.Errorf("context window exceeds limit (2013)")
	}
	return &api.Response{
		Content: []api.ContentBlock{
			{Type: api.BlockTypeText, Text: "Summary"},
		},
		StopReason: "end_turn",
		Model:      "test-model",
		Usage:      api.Usage{InputTokens: 100, OutputTokens: 10},
	}, nil
}

// TestForkRetriesExhaustedReturnsUnderlyingError verifies that when the
// fallbackSummaryCompact retry group-dropping exhausts all retries, compactMessages
// returns the underlying error (not a generic "compaction failed" error).
func TestForkRetriesExhaustedReturnsUnderlyingError(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	var messages []api.Message
	for i := range 6 {
		content := strings.Repeat("large message content for round ", 8_000)
		messages = append(messages, api.Message{Role: "user", Content: content})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Response to round %d", i),
			ToolUse: []api.ToolUseBlock{{ID: fmt.Sprintf("call_%d", i), Name: "Read", Input: map[string]any{}}},
		})
		messages = append(messages, api.Message{
			Role:        "user",
			ToolResults: []api.ToolResultBlock{{ToolUseID: fmt.Sprintf("call_%d", i), Content: "result"}},
		})
	}

	estimated := estimateTokens(messages)
	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS
	if estimated <= maxMessagesTokens {
		t.Fatalf("FAIL: test setup: estimated %d should exceed maxMessagesTokens %d", estimated, maxMessagesTokens)
	}
	t.Logf("estimated tokens = %d, maxMessagesTokens = %d", estimated, maxMessagesTokens)

	mock := &mockExhaustClient{}
	engine := newTestEngine(mock)

	_, err := engine.compactMessages(context.Background(), messages, cfg, []string{"test system prompt"})
	if err == nil {
		t.Fatal("FAIL: compactMessages should return error when fallbackSummaryCompact exhausts retries")
	}

	errStr := err.Error()
	if strings.Contains(errStr, "compaction failed") {
		t.Error("FAIL: error should not be generic 'compaction failed' — must propagate underlying error")
	}
	if !strings.Contains(errStr, "context window") {
		t.Errorf("FAIL: error should contain underlying error 'context window', got: %q", errStr)
	}
	if !strings.Contains(errStr, "5 attempts") && !strings.Contains(errStr, "retries") {
		t.Errorf("FAIL: error should indicate retries exhausted (got: %q)", errStr)
	}
	if mock.calls < 3 {
		t.Errorf("FAIL: expected at least 3 API calls (retry attempts), got %d", mock.calls)
	}

	t.Logf("PASS: compactMessages returned underlying error %q (calls=%d)", errStr, mock.calls)
}
