package agent

import (
	"fmt"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

func TestProgressMonitor_ScoreTurn_FileWrite(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "write", Input: map[string]any{"file_path": "/workspace/main.go"}}},
		[]api.ToolResult{{Content: "ok"}},
	)
	if ts.Score != 3 {
		t.Errorf("expected score 3 for file write, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_ScratchpadWrite(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "write", Input: map[string]any{"file_path": "/tmp/JENNY_SCRATCHPAD/notes.md"}}},
		[]api.ToolResult{{Content: "ok"}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for scratchpad write, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_NewRead(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Read", Input: map[string]any{"file_path": "/workspace/go.mod"}}},
		[]api.ToolResult{{Content: "module foo"}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for new read, got %d", ts.Score)
	}

	// Reading same file again should not score
	ts2 := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Read", Input: map[string]any{"file_path": "/workspace/go.mod"}}},
		[]api.ToolResult{{Content: "module foo"}},
	)
	if ts2.Score != 0 {
		t.Errorf("expected score 0 for repeated read, got %d", ts2.Score)
	}
}

func TestProgressMonitor_ScoreTurn_BashFailed(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "make build"}}},
		[]api.ToolResult{{Content: "error: undefined", IsError: true}},
	)
	if ts.Score != -1 {
		t.Errorf("expected score -1 for failed bash, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_RepeatedCommand(t *testing.T) {
	pm := NewProgressMonitor()
	pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "make build"}}},
		[]api.ToolResult{{Content: "error: undefined", IsError: true}},
	)

	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "make build"}}},
		[]api.ToolResult{{Content: "error: undefined", IsError: true}},
	)
	// -1 (failed) + -2 (repeated command) + -2 (repeated error) = -5
	if ts.Score != -5 {
		t.Errorf("expected score -5 for repeated failed command, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_TestPass(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "go test ./..."}}},
		[]api.ToolResult{{Content: "ok  all tests passed"}},
	)
	if ts.Score != 3 {
		t.Errorf("expected score 3 for test pass, got %d", ts.Score)
	}
}

func TestProgressMonitor_L1_After10NoProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "fail"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1Threshold; i++ {
		// Use different commands each turn to avoid repeated command penalty
		failBlock[0].Input["command"] = string(rune('a' + i))
		pm.RecordTurn(failBlock, failResult)
	}

	if pm.Level() != ProgressLevelL1 {
		t.Errorf("expected L1 after %d no-progress turns, got level %d", l1Threshold, pm.Level())
	}

	msg, terminate := pm.CheckAndInject()
	if terminate {
		t.Error("L1 should not terminate")
	}
	if msg == nil {
		t.Fatal("L1 should inject a message")
	}
	if msg.Role != api.RoleUser {
		t.Errorf("expected user role, got %s", msg.Role)
	}
	if !msg.IsVirtual {
		t.Error("L1 message should be virtual")
	}
}

func TestProgressMonitor_L1_OnlyFiresOnce(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1Threshold; i++ {
		failBlock[0].Input["command"] = string(rune('a' + i))
		pm.RecordTurn(failBlock, failResult)
	}

	msg1, _ := pm.CheckAndInject()
	if msg1 == nil {
		t.Fatal("L1 should fire first time")
	}

	msg2, _ := pm.CheckAndInject()
	if msg2 != nil {
		t.Error("L1 should not fire again")
	}
}

func TestProgressMonitor_L2_After20NoProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l2Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
	}

	if pm.Level() != ProgressLevelL2 {
		t.Errorf("expected L2 after %d no-progress turns, got level %d", l2Threshold, pm.Level())
	}

	msg, terminate := pm.CheckAndInject()
	if terminate {
		t.Error("L2 should not immediately terminate")
	}
	if msg == nil {
		t.Fatal("L2 should inject a message")
	}
}

func TestProgressMonitor_L3_After25NoProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l3Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
	}

	if pm.Level() != ProgressLevelL3 {
		t.Errorf("expected L3 after %d no-progress turns, got level %d", l3Threshold, pm.Level())
	}

	_, terminate := pm.CheckAndInject()
	if !terminate {
		t.Error("L3 should terminate")
	}
}

func TestProgressMonitor_ResetOnProgress(t *testing.T) {
	pm := NewProgressMonitor()

	// Use different error messages to avoid repeated_error penalty accumulation
	for i := 0; i < 5; i++ {
		pm.RecordTurn(
			[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": fmt.Sprintf("cmd_%d", i)}}},
			[]api.ToolResult{{Content: fmt.Sprintf("error_%d", i), IsError: true}},
		)
	}

	if pm.ConsecutiveNoProgress() != 5 {
		t.Errorf("expected 5 consecutive no-progress, got %d", pm.ConsecutiveNoProgress())
	}

	// Progress signal: file write
	pm.RecordTurn(
		[]api.ToolUseBlock{{Name: "write", Input: map[string]any{"file_path": "/workspace/out.txt"}}},
		[]api.ToolResult{{Content: "ok"}},
	)

	if pm.ConsecutiveNoProgress() != 0 {
		t.Errorf("expected 0 consecutive after progress, got %d", pm.ConsecutiveNoProgress())
	}
	if pm.Level() != ProgressLevelNone {
		t.Errorf("expected level 0 after progress, got %d", pm.Level())
	}
}

func TestProgressMonitor_SlidingWindowTrigger(t *testing.T) {
	pm := NewProgressMonitor()

	// Push 10 turns with score -1 each (sum = -10)
	for i := 0; i < slidingWindowSize; i++ {
		pm.RecordTurn(
			[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": fmt.Sprintf("cmd_%d", i)}}},
			[]api.ToolResult{{Content: "error", IsError: true}},
		)
	}

	if pm.Level() != ProgressLevelL1 {
		t.Errorf("expected L1 from sliding window, got level %d", pm.Level())
	}
}

func TestProgressMonitor_L2_EscalatesToL3AfterTurns(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l2Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
	}

	// First check fires L2 message
	msg, term := pm.CheckAndInject()
	if msg == nil || term {
		t.Fatal("first L2 check should return message, not terminate")
	}

	// Subsequent checks count turns
	for i := 0; i < l2MaxToolCalls; i++ {
		_, term = pm.CheckAndInject()
		if term {
			t.Errorf("should not terminate at turn %d", i+1)
		}
	}

	// Next check should escalate to L3
	_, term = pm.CheckAndInject()
	if !term {
		t.Error("should terminate after exceeding L2 max turns")
	}
}

func TestProgressMonitor_WeakSignalCannotOvercomeFail(t *testing.T) {
	pm := NewProgressMonitor()

	// Turn with bash fail (-1) + repeated error (-2) + scratchpad write (+1) = -2
	// First, prime with a failure
	pm.RecordTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "make"}}},
		[]api.ToolResult{{Content: "error: foo", IsError: true}},
	)
	// Same bash fail + scratchpad write
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{
			{Name: "Bash", Input: map[string]any{"command": "make"}},
			{Name: "write", Input: map[string]any{"file_path": "/tmp/JENNY_SCRATCHPAD/note.md"}},
		},
		[]api.ToolResult{
			{Content: "error: foo", IsError: true},
			{Content: "ok"},
		},
	)
	// -1 (fail) + -2 (repeated cmd) + -2 (repeated error) + 1 (scratchpad) = -4
	if ts.Score > 0 {
		t.Errorf("weak signal should not make overall score positive, got %d", ts.Score)
	}
}
