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

	ts2 := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Read", Input: map[string]any{"file_path": "/workspace/go.mod"}}},
		[]api.ToolResult{{Content: "module foo"}},
	)
	if ts2.Score != 0 {
		t.Errorf("expected score 0 for repeated read, got %d", ts2.Score)
	}
}

func TestProgressMonitor_ScoreTurn_ReadPathsCapped(t *testing.T) {
	pm := NewProgressMonitor()
	for i := 0; i < maxTrackedReadPaths+10; i++ {
		pm.scoreTurn(
			[]api.ToolUseBlock{{Name: "Read", Input: map[string]any{"file_path": fmt.Sprintf("/workspace/file_%d.go", i)}}},
			[]api.ToolResult{{Content: "ok"}},
		)
	}
	if len(pm.previouslyReadPaths) > maxTrackedReadPaths {
		t.Errorf("expected at most %d tracked read paths, got %d", maxTrackedReadPaths, len(pm.previouslyReadPaths))
	}
	// FIFO: earliest paths should have been evicted
	if pm.previouslyReadPaths["/workspace/file_0.go"] {
		t.Error("expected file_0.go to be evicted (FIFO)")
	}
	if !pm.previouslyReadPaths[fmt.Sprintf("/workspace/file_%d.go", maxTrackedReadPaths+9)] {
		t.Error("expected latest file to be present")
	}
	// Re-reading an evicted path should score as new
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Read", Input: map[string]any{"file_path": "/workspace/file_0.go"}}},
		[]api.ToolResult{{Content: "ok"}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for re-reading evicted path, got %d", ts.Score)
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

func TestProgressMonitor_ScoreTurn_TestPass_GoTest(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "go test ./..."}}},
		[]api.ToolResult{{Content: "ok  \tgithub.com/foo/bar\t0.003s"}},
	)
	if ts.Score != 3 {
		t.Errorf("expected score 3 for go test pass, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_TestPass_Pytest(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "pytest tests/"}}},
		[]api.ToolResult{{Content: "=== 5 passed ==="}},
	)
	if ts.Score != 3 {
		t.Errorf("expected score 3 for pytest pass, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_BuildSuccess_Make(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "make -j4"}}},
		[]api.ToolResult{{Content: "cc -o main main.c"}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for make success, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_BuildSuccess_GCC(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "gcc -o main main.c"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for gcc success, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_BuildSuccess_Configure(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "./configure --enable-gcov"}}},
		[]api.ToolResult{{Content: "configure: creating config.h"}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for configure success, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_PackageInstall_AptGet(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "apt-get install -y build-essential"}}},
		[]api.ToolResult{{Content: "Setting up gcc..."}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for apt-get install, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_PackageInstall_SettingUp(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "dpkg --configure -a"}}},
		[]api.ToolResult{{Content: "Setting up gcc-12 (12.3.0-1)\nSetting up g++ (12.3.0-1)\nSetting up build-essential (12.3.0-1)\n"}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for Setting up detection, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_NoBroadPatternMatching(t *testing.T) {
	pm := NewProgressMonitor()

	// "ok" in random output should NOT trigger test_pass
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "cat status.txt"}}},
		[]api.ToolResult{{Content: "everything looks ok for now"}},
	)
	if ts.Score != 0 {
		t.Errorf("expected score 0 for non-test command with 'ok', got %d (signals: %v)", ts.Score, ts.Signals)
	}

	// " pass " in random output should NOT trigger test_pass
	ts2 := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "echo 'this will pass the check'"}}},
		[]api.ToolResult{{Content: "this will pass the check"}},
	)
	if ts2.Score != 0 {
		t.Errorf("expected score 0 for non-test command with 'pass', got %d (signals: %v)", ts2.Score, ts2.Signals)
	}

	// " success " in random output should NOT trigger
	ts3 := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "echo success"}}},
		[]api.ToolResult{{Content: "operation completed with success"}},
	)
	if ts3.Score != 0 {
		t.Errorf("expected score 0 for non-test command with 'success', got %d (signals: %v)", ts3.Score, ts3.Signals)
	}
}

func TestProgressMonitor_ScoreTurn_PrefixWordBoundary(t *testing.T) {
	pm := NewProgressMonitor()

	// "makefile" should NOT match "make" (no word boundary)
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "makefile --help"}}},
		[]api.ToolResult{{Content: "usage: makefile"}},
	)
	if ts.Score != 0 {
		t.Errorf("expected score 0 for 'makefile' (not a build command), got %d (signals: %v)", ts.Score, ts.Signals)
	}

	// "go testing" should NOT match "go test"
	ts2 := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "go testing"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts2.Score != 0 {
		t.Errorf("expected score 0 for 'go testing' (not a test command), got %d (signals: %v)", ts2.Score, ts2.Signals)
	}

	// "gcc" alone should match (exact)
	ts3 := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "gcc"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts3.Score != 2 {
		t.Errorf("expected score 2 for 'gcc' (exact match), got %d (signals: %v)", ts3.Score, ts3.Signals)
	}
}

func TestProgressMonitor_ScoreTurn_FailedTestRunnerIsNegative(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "go test ./..."}}},
		[]api.ToolResult{{Content: "FAIL github.com/foo/bar", IsError: true}},
	)
	if ts.Score >= 0 {
		t.Errorf("expected negative score for failed test runner, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_PackageInstall_PipOutput(t *testing.T) {
	pm := NewProgressMonitor()
	// Use a wrapped command so the prefix check doesn't match directly
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "sudo pip3 install requests"}}},
		[]api.ToolResult{{Content: "Successfully installed requests-2.31.0 urllib3-2.1.0"}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for pip install output detection, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_PackageInstall_NpmOutput(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "npm i --save-dev jest"}}},
		[]api.ToolResult{{Content: "added 274 packages in 5.2s"}},
	)
	if ts.Score != 1 {
		t.Errorf("expected score 1 for npm install output detection, got %d", ts.Score)
	}
}

func TestProgressMonitor_L1First_After10NoProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "fail"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1FirstThreshold; i++ {
		failBlock[0].Input["command"] = string(rune('a' + i))
		pm.RecordTurn(failBlock, failResult)
	}

	if pm.Level() != ProgressLevelL1 {
		t.Errorf("expected L1 after %d no-progress turns, got level %d", l1FirstThreshold, pm.Level())
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

func TestProgressMonitor_L1First_OnlyFiresOnce(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1FirstThreshold; i++ {
		failBlock[0].Input["command"] = string(rune('a' + i))
		pm.RecordTurn(failBlock, failResult)
	}

	msg1, _ := pm.CheckAndInject()
	if msg1 == nil {
		t.Fatal("L1 first should fire first time")
	}

	msg2, _ := pm.CheckAndInject()
	if msg2 != nil {
		t.Error("L1 first should not fire again")
	}
}

func TestProgressMonitor_L1Second_At20(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1SecondThreshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		if i == l1FirstThreshold-1 {
			pm.CheckAndInject() // consume first L1
		}
	}

	if pm.Level() != ProgressLevelL1 {
		t.Errorf("expected L1 at %d turns (not L2 yet), got level %d", l1SecondThreshold, pm.Level())
	}

	msg, terminate := pm.CheckAndInject()
	if terminate {
		t.Error("L1 second should not terminate")
	}
	if msg == nil {
		t.Fatal("L1 second reminder should fire at 20 turns")
	}
	if msg.Content != l1SecondReminderText {
		t.Error("expected second L1 reminder text")
	}
}

func TestProgressMonitor_L2_After25NoProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l2Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		// Consume L1 reminders
		if i == l1FirstThreshold-1 || i == l1SecondThreshold-1 {
			pm.CheckAndInject()
		}
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

func TestProgressMonitor_L3_After30NoProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l3Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		// Consume L1 and L2 reminders
		if i == l1FirstThreshold-1 || i == l1SecondThreshold-1 || i == l2Threshold-1 {
			pm.CheckAndInject()
		}
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

	for i := 0; i < 5; i++ {
		pm.RecordTurn(
			[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": fmt.Sprintf("cmd_%d", i)}}},
			[]api.ToolResult{{Content: fmt.Sprintf("error_%d", i), IsError: true}},
		)
	}

	if pm.ConsecutiveNoProgress() != 5 {
		t.Errorf("expected 5 consecutive no-progress, got %d", pm.ConsecutiveNoProgress())
	}

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

func TestProgressMonitor_SlidingWindowCannotReachL2(t *testing.T) {
	pm := NewProgressMonitor()

	// Alternate between heavy failures and a reset to keep consecutive count low
	// but sliding window deeply negative.
	for cycle := 0; cycle < 5; cycle++ {
		for i := 0; i < 8; i++ {
			pm.RecordTurn(
				[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": fmt.Sprintf("cmd_%d_%d", cycle, i)}}},
				[]api.ToolResult{{Content: "error", IsError: true}},
			)
		}
		// Reset consecutive counter with a positive signal
		pm.RecordTurn(
			[]api.ToolUseBlock{{Name: "write", Input: map[string]any{"file_path": fmt.Sprintf("/workspace/out_%d.txt", cycle)}}},
			[]api.ToolResult{{Content: "ok"}},
		)
	}

	// Sliding window should be negative, but level should never exceed L1
	if pm.Level() > ProgressLevelL1 {
		t.Errorf("sliding window should only trigger L1, got level %d", pm.Level())
	}
}

func TestProgressMonitor_L2_EscalatesToL3AfterTurns(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l2Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		// Consume L1 reminders
		if i == l1FirstThreshold-1 || i == l1SecondThreshold-1 {
			pm.CheckAndInject()
		}
	}

	// First check fires L2 message
	msg, term := pm.CheckAndInject()
	if msg == nil || term {
		t.Fatal("first L2 check should return message, not terminate")
	}

	// Subsequent checks count turns
	for i := 0; i < l2MaxTurns; i++ {
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

	pm.RecordTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "make"}}},
		[]api.ToolResult{{Content: "error: foo", IsError: true}},
	)
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
