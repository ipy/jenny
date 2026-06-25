package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

func TestLoopGuard_CanonicalArgsKeyOrder(t *testing.T) {
	a := map[string]any{"a": 1, "b": 2}
	b := map[string]any{"b": 2, "a": 1}
	if canonicalArgsHash("Read", a) != canonicalArgsHash("Read", b) {
		t.Fatal("expected identical hash for reordered keys")
	}
}

func TestLoopGuard_CanonicalArgsWhitespace(t *testing.T) {
	a := map[string]any{"command": "  ls -la  "}
	b := map[string]any{"command": "ls -la"}
	if canonicalArgsHash("Bash", a) != canonicalArgsHash("Bash", b) {
		t.Fatal("expected identical hash after whitespace normalization")
	}
}

func TestLoopGuard_ToolSoftThresholdWarning(t *testing.T) {
	lg := NewLoopGuard()
	block := api.ToolUseBlock{Name: "Read", Input: map[string]any{"file_path": "/tmp/x.go"}}

	for i := 0; i < 2; i++ {
		decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
		if len(decisions) != 1 || decisions[0].Action != ToolActionExecute {
			t.Fatalf("turn %d: expected execute, got %+v", i+1, decisions[0])
		}
		if decisions[0].Warning != "" {
			t.Fatalf("turn %d: unexpected warning", i+1)
		}
	}

	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	if decisions[0].Action != ToolActionExecute {
		t.Fatalf("expected execute at soft threshold, got %v", decisions[0].Action)
	}
	if decisions[0].Warning == "" {
		t.Fatal("expected soft-threshold warning")
	}
	if !strings.Contains(decisions[0].Warning, "system-reminder") {
		t.Fatal("warning should be system-reminder formatted")
	}
}

func TestLoopGuard_ToolHardThresholdBlock(t *testing.T) {
	lg := NewLoopGuard()
	block := api.ToolUseBlock{Name: "Bash", Input: map[string]any{"command": "go test ./..."}}

	for i := 0; i < 5; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}

	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].Action != ToolActionBlock {
		t.Fatalf("expected block at hard threshold, got %v", decisions[0].Action)
	}
	if decisions[0].BlockMsg == "" {
		t.Fatal("expected block message")
	}
}

func TestLoopGuard_NoBatchDedup(t *testing.T) {
	lg := NewLoopGuard()
	block := api.ToolUseBlock{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go"}}
	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block, block, block})

	for i, d := range decisions {
		if d.Action != ToolActionExecute {
			t.Fatalf("decision[%d] should execute (no batch dedup), got %v", i, d.Action)
		}
	}
}

func TestLoopGuard_NotifyFileWriteResetsCounter(t *testing.T) {
	lg := NewLoopGuard()
	path := "/workspace/new.go"
	block := api.ToolUseBlock{Name: "write", Input: map[string]any{"file_path": path}}

	for i := 0; i < 3; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}
	lg.NotifyFileWrite(path)

	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	if decisions[0].Warning != "" {
		t.Fatal("expected counter reset after new file write, no warning")
	}
}

func TestLoopGuard_NotifyFileWriteSamePathNoReset(t *testing.T) {
	lg := NewLoopGuard()
	path := "/workspace/existing.go"
	block := api.ToolUseBlock{Name: "write", Input: map[string]any{"file_path": path}}

	for i := 0; i < 3; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}
	lg.NotifyFileWrite(path)
	for i := 0; i < 2; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}
	lg.NotifyFileWrite(path)

	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	if decisions[0].Warning == "" {
		t.Fatal("expected warning after rewrite to already-written path")
	}
}

func TestLoopGuard_AssistantTextDirectRepetition(t *testing.T) {
	lg := NewLoopGuard()
	text := "Let me read the file again."

	score, signals := lg.RecordAssistantText(text)
	if score != 0 || len(signals) != 0 {
		t.Fatalf("first occurrence should not signal, score=%d signals=%v", score, signals)
	}

	score, signals = lg.RecordAssistantText(text)
	if score != 0 || len(signals) != 0 {
		t.Fatalf("second occurrence should not signal, score=%d signals=%v", score, signals)
	}

	score, signals = lg.RecordAssistantText(text)
	if score != -5 {
		t.Fatalf("third consecutive repeat expected -5, got %d", score)
	}
	if len(signals) == 0 || !strings.Contains(signals[0], "direct_text_repeat") {
		t.Fatalf("expected direct_text_repeat signal, got %v", signals)
	}
}

func TestLoopGuard_AssistantTextNormalization(t *testing.T) {
	lg := NewLoopGuard()
	lg.RecordAssistantText("Hello World")
	lg.RecordAssistantText("  hello   world  ")
	score, _ := lg.RecordAssistantText("\nHELLO WORLD\n")
	if score != -5 {
		t.Fatalf("normalized repeats should score -5, got %d", score)
	}
}

func TestLoopGuard_AssistantTextCyclicRepetition(t *testing.T) {
	lg := NewLoopGuard()
	// Two repetitions should NOT trigger (minCycleRepetitions = 3)
	texts2 := []string{"step one", "step two", "step three", "step one", "step two", "step three"}
	var scoreAt2 int
	for _, text := range texts2 {
		scoreAt2, _ = lg.RecordAssistantText(text)
	}
	if scoreAt2 != 0 {
		t.Fatalf("expected no detection at 2 repetitions, got score %d", scoreAt2)
	}

	// Third repetition triggers
	texts3 := []string{"step one", "step two", "step three"}
	var lastScore int
	for _, text := range texts3 {
		lastScore, _ = lg.RecordAssistantText(text)
	}
	if lastScore != -5 {
		t.Fatalf("expected cyclic repetition score -5, got %d", lastScore)
	}
}

func TestLoopGuard_AssistantTextCycleLengthTwo(t *testing.T) {
	lg := NewLoopGuard()
	// A, B, A, B (2 repetitions) should NOT trigger
	for _, text := range []string{"alpha", "beta", "alpha", "beta"} {
		score, _ := lg.RecordAssistantText(text)
		if score != 0 {
			t.Fatalf("expected no detection at 2 repetitions of length-2 cycle, got score %d", score)
		}
	}
	// A, B again → 3 repetitions → triggers
	for _, text := range []string{"alpha", "beta"} {
		score, signals := lg.RecordAssistantText(text)
		if text == "beta" && score == -5 {
			if len(signals) == 0 || !strings.Contains(signals[0], "cyclic_text_repeat") {
				t.Fatalf("expected cyclic_text_repeat signal, got %v", signals)
			}
			return
		}
	}
	t.Fatal("expected cyclic detection on third repetition of length-2 cycle")
}

func TestLoopGuard_ToolTurnScore(t *testing.T) {
	lg := NewLoopGuard()
	block := api.ToolUseBlock{Name: "Read", Input: map[string]any{"file_path": "/x.go"}}
	for i := 0; i < 2; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}
	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	ts := lg.ToolTurnScore(decisions)
	if ts.Score >= 0 {
		t.Fatalf("expected negative score at soft threshold, got %d", ts.Score)
	}
	if len(ts.Signals) == 0 {
		t.Fatal("expected repetition signals")
	}
}

func TestLoopGuard_Reset(t *testing.T) {
	lg := NewLoopGuard()
	block := api.ToolUseBlock{Name: "Read", Input: map[string]any{"file_path": "/a.go"}}
	for i := 0; i < 3; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}
	lg.RecordAssistantText("repeat me")
	lg.RecordAssistantText("repeat me")
	lg.RecordAssistantText("repeat me")
	lg.NotifyFileWrite("/workspace/out.go")

	lg.Reset()

	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	if decisions[0].Warning != "" {
		t.Fatal("expected tool counter cleared after reset")
	}
	score, _ := lg.RecordAssistantText("repeat me")
	if score != 0 {
		t.Fatalf("expected text window cleared after reset, got score %d", score)
	}
}

func TestLoopGuard_LRUEviction(t *testing.T) {
	lg := NewLoopGuard()
	firstBlock := api.ToolUseBlock{Name: "Read", Input: map[string]any{"file_path": "/evict_me.go"}}
	for i := 0; i < toolSoftThreshold; i++ {
		lg.EvaluateToolCalls([]api.ToolUseBlock{firstBlock})
	}

	for i := 0; i < maxToolCallEntries; i++ {
		block := api.ToolUseBlock{
			Name:  "Read",
			Input: map[string]any{"file_path": fmt.Sprintf("/other_%d.go", i)},
		}
		lg.EvaluateToolCalls([]api.ToolUseBlock{block})
	}

	decisions := lg.EvaluateToolCalls([]api.ToolUseBlock{firstBlock})
	if decisions[0].Warning != "" {
		t.Fatal("evicted entry should not retain soft-threshold count")
	}
}

func TestProgressMonitor_RecordTurnWithSupplemental(t *testing.T) {
	pm := NewProgressMonitor()
	// Force no-progress state
	for i := 0; i < 5; i++ {
		pm.RecordTurn(
			[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "false"}}},
			[]api.ToolResult{{IsError: true, Content: "fail"}},
		)
	}
	before := pm.ConsecutiveNoProgress()

	ts := pm.RecordTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "false"}}},
		[]api.ToolResult{{IsError: true, Content: "fail"}},
		TurnScore{Score: -5, Signals: []string{"direct_text_repeat"}},
	)
	if ts.Score >= 0 {
		t.Fatalf("expected negative combined score, got %d", ts.Score)
	}
	if pm.ConsecutiveNoProgress() <= before {
		t.Fatal("supplemental negative score should increment no-progress counter")
	}
}
