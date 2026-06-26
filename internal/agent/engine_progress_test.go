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
		t.Errorf("expected score 3 for first file write, got %d", ts.Score)
	}
}

func TestProgressMonitor_ScoreTurn_FileRewriteDecay(t *testing.T) {
	pm := NewProgressMonitor()
	path := "/workspace/results.json"
	block := []api.ToolUseBlock{{Name: "write", Input: map[string]any{"file_path": path}}}
	result := []api.ToolResult{{Content: "ok"}}

	// 1st write: +3
	ts1 := pm.scoreTurn(block, result)
	if ts1.Score != 3 {
		t.Errorf("1st write: expected +3, got %d", ts1.Score)
	}

	// 2nd write: +1
	ts2 := pm.scoreTurn(block, result)
	if ts2.Score != 1 {
		t.Errorf("2nd write: expected +1, got %d", ts2.Score)
	}

	// 3rd write: 0
	ts3 := pm.scoreTurn(block, result)
	if ts3.Score != 0 {
		t.Errorf("3rd write: expected 0, got %d", ts3.Score)
	}

	// 4th write: -1
	ts4 := pm.scoreTurn(block, result)
	if ts4.Score != -1 {
		t.Errorf("4th write: expected -1, got %d", ts4.Score)
	}

	// 5th write: still -1 (capped)
	ts5 := pm.scoreTurn(block, result)
	if ts5.Score != -1 {
		t.Errorf("5th write: expected -1, got %d", ts5.Score)
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

func TestProgressMonitor_ScoreTurn_BuildSuccess_CompoundGCC(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "cd /app && gcc -o main main.c"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for compound gcc command, got %d (signals: %v)", ts.Score, ts.Signals)
	}
}

func TestProgressMonitor_ScoreTurn_BuildSuccess_GCCVersioned(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "gcc-12 -O3 -fPIC -shared -o lib.so lib.c"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for gcc-12 versioned command, got %d (signals: %v)", ts.Score, ts.Signals)
	}
}

func TestProgressMonitor_ScoreTurn_BuildSuccess_CompoundGCCVersioned(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "cd /app && gcc-12 -O3 -fPIC -shared -o ext.so ext.c"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for cd+gcc-12, got %d (signals: %v)", ts.Score, ts.Signals)
	}
}

func TestProgressMonitor_ScoreTurn_TestPass_CompoundGoTest(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "cd /workspace && go test ./..."}}},
		[]api.ToolResult{{Content: "ok  \tgithub.com/foo/bar\t0.003s"}},
	)
	if ts.Score != 3 {
		t.Errorf("expected score 3 for compound go test, got %d (signals: %v)", ts.Score, ts.Signals)
	}
}

func TestProgressMonitor_ScoreTurn_CompoundSemicolon(t *testing.T) {
	pm := NewProgressMonitor()
	ts := pm.scoreTurn(
		[]api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "export CC=gcc; make -j4"}}},
		[]api.ToolResult{{Content: ""}},
	)
	if ts.Score != 2 {
		t.Errorf("expected score 2 for semicolon compound with make, got %d (signals: %v)", ts.Score, ts.Signals)
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

	// Main loop pattern: dispatch fork BEFORE CheckAndInject
	if !pm.ShouldForkL3ProgressEval() {
		t.Fatal("ShouldForkL3ProgressEval should be true when L3 first reached")
	}
	pm.MarkL3ForkDispatched()

	// L3 defers termination while fork is in flight
	_, terminate := pm.CheckAndInject()
	if terminate {
		t.Error("L3 should defer while fork is in flight")
	}

	// Simulate negative fork result
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:    1,
		OutcomeMomentum:  1,
		FreshExploration: 1,
		Overall:          1.0,
		Reasoning:        "stuck",
	}
	pm.DrainForkResult()

	// Now L3 should terminate (fork returned negative)
	_, terminate = pm.CheckAndInject()
	if !terminate {
		t.Error("L3 should terminate after negative fork-judge")
	}
}

func TestProgressMonitor_L3_ForkJudgePositiveRescues(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l3Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		if i == l1FirstThreshold-1 || i == l1SecondThreshold-1 || i == l2Threshold-1 {
			pm.CheckAndInject()
		}
	}

	// L3 defers termination
	if !pm.ShouldForkL3ProgressEval() {
		t.Fatal("ShouldForkL3ProgressEval should be true at L3")
	}
	pm.MarkL3ForkDispatched()

	// Fork is in flight → defer
	_, terminate := pm.CheckAndInject()
	if terminate {
		t.Error("L3 should defer while fork is in flight")
	}

	// Positive fork result → rescue (MeaningfulProgress is computed by
	// parseProgressEvalResponse; tests that bypass parsing must set it explicitly)
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:      4,
		OutcomeMomentum:    3,
		FreshExploration:   3,
		Overall:            3.55,
		MeaningfulProgress: true,
		Reasoning:          "making real progress",
	}
	pm.DrainForkResult()

	if pm.Level() != ProgressLevelNone {
		t.Errorf("expected level 0 after positive L3 fork, got %d", pm.Level())
	}
	if pm.ConsecutiveNoProgress() != 0 {
		t.Errorf("expected 0 consecutive after rescue, got %d", pm.ConsecutiveNoProgress())
	}
	_, terminate = pm.CheckAndInject()
	if terminate {
		t.Error("should not terminate after positive fork rescue")
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

	// L2→L3 escalation: defers to let fork-judge dispatch
	_, term = pm.CheckAndInject()
	if term {
		t.Error("L2→L3 escalation should defer (not immediate terminate) to allow fork-judge")
	}
	if pm.Level() != ProgressLevelL3 {
		t.Errorf("expected L3 after exceeding L2 max turns, got %d", pm.Level())
	}

	// Simulate negative fork-judge → terminate
	pm.MarkL3ForkDispatched()
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:    1,
		OutcomeMomentum:  1,
		FreshExploration: 1,
		Overall:          1.0,
		Reasoning:        "stuck",
	}
	pm.DrainForkResult()

	_, term = pm.CheckAndInject()
	if !term {
		t.Error("should terminate after negative L3 fork-judge")
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

func TestParseProgressEvalResponse_MeaningfulProgress(t *testing.T) {
	raw := `{"task_alignment":4,"outcome_momentum":3,"fresh_exploration":4,"overall":3.65,"meaningful_progress":true,"reasoning":"Agent is systematically profiling each slow query."}`
	result, err := parseProgressEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// parseProgressEvalResponse computes MeaningfulProgress from the weighted
	// average vs forkJudgeThreshold (3.0). With scores 4/3/4, overall=3.65 >= 3.0.
	if !result.MeaningfulProgress {
		t.Errorf("expected meaningful_progress=true for overall=%.2f >= threshold", result.Overall)
	}
	expectedOverall := float64(4)*0.50 + float64(3)*0.35 + float64(4)*0.15
	if result.Overall != expectedOverall {
		t.Errorf("expected overall=%.2f, got %.2f", expectedOverall, result.Overall)
	}
	if result.TaskAlignment != 4 {
		t.Errorf("expected task_alignment=4, got %d", result.TaskAlignment)
	}
	if result.OutcomeMomentum != 3 {
		t.Errorf("expected outcome_momentum=3, got %d", result.OutcomeMomentum)
	}
	if result.FreshExploration != 4 {
		t.Errorf("expected fresh_exploration=4, got %d", result.FreshExploration)
	}
}

func TestParseProgressEvalResponse_NoProgress(t *testing.T) {
	raw := `{"task_alignment":1,"outcome_momentum":2,"fresh_exploration":1,"overall":1.35,"meaningful_progress":false,"reasoning":"Agent is looping through the same error without changing approach."}`
	result, err := parseProgressEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MeaningfulProgress {
		t.Error("expected meaningful_progress=false")
	}
}

func TestParseProgressEvalResponse_WithFences(t *testing.T) {
	raw := "```json\n{\"task_alignment\":5,\"outcome_momentum\":4,\"fresh_exploration\":5,\"overall\":4.65,\"meaningful_progress\":true,\"reasoning\":\"Agent is following user instructions exactly.\"}\n```"
	result, err := parseProgressEvalResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With scores 5/4/5, overall = 5*0.50 + 4*0.35 + 5*0.15 = 4.65 >= 3.0 threshold
	if !result.MeaningfulProgress {
		t.Errorf("expected meaningful_progress=true for overall=%.2f >= threshold", result.Overall)
	}
	expectedOverall := float64(5)*0.50 + float64(4)*0.35 + float64(5)*0.15
	if diff := result.Overall - expectedOverall; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected overall≈%.2f, got %.20f", expectedOverall, result.Overall)
	}
}

func TestParseProgressEvalResponse_InvalidJSON(t *testing.T) {
	_, err := parseProgressEvalResponse("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestProgressMonitor_L1SecondForkJudge_MeaningfulProgressResets(t *testing.T) {
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

	if pm.ConsecutiveNoProgress() != l1SecondThreshold {
		t.Fatalf("expected %d consecutive, got %d", l1SecondThreshold, pm.ConsecutiveNoProgress())
	}

	// Simulate fork via channel + drain (matching the real main loop pattern)
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:      4,
		OutcomeMomentum:    3,
		FreshExploration:   4,
		Overall:            3.65,
		MeaningfulProgress: true,
		Reasoning:          "Agent is systematically profiling queries as user requested.",
	}
	pm.DrainForkResult()

	if pm.ConsecutiveNoProgress() != 0 {
		t.Errorf("expected 0 consecutive after positive judge, got %d", pm.ConsecutiveNoProgress())
	}
	if pm.Level() != ProgressLevelNone {
		t.Errorf("expected level 0 after positive judge, got %d", pm.Level())
	}
	if pm.l1FirstFired {
		t.Error("l1FirstFired should be false after full reset")
	}
	if pm.l1SecondFired {
		t.Error("l1SecondFired should be false after full reset")
	}
}

func TestProgressMonitor_L1SecondForkJudge_NoProgressFallsThrough(t *testing.T) {
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

	// Simulate fork via channel + drain
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:      1,
		OutcomeMomentum:    2,
		FreshExploration:   1,
		Overall:            1.35,
		MeaningfulProgress: false,
		Reasoning:          "Agent is looping through the same error.",
	}
	pm.DrainForkResult()

	// Counter should NOT be reset
	if pm.ConsecutiveNoProgress() < l1SecondThreshold {
		t.Errorf("expected at least %d consecutive after negative judge, got %d", l1SecondThreshold, pm.ConsecutiveNoProgress())
	}
	// L1 second reminder should still fire
	msg, terminate := pm.CheckAndInject()
	if terminate {
		t.Error("L1 second should not terminate after negative judge")
	}
	if msg == nil {
		t.Fatal("L1 second reminder should fire after negative judge")
	}
}

func TestProgressMonitor_L1SecondForkJudge_OnlyFiresOnce(t *testing.T) {
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

	// Fork fires at L1-second
	if !pm.ShouldForkProgressEval() {
		t.Fatal("L1-second trigger should request fork")
	}

	// Consume the fork opportunity
	pm.MarkForkDispatched()

	// Should not request again
	if pm.ShouldForkProgressEval() {
		t.Error("fork should not be requested twice")
	}
}

func TestProgressMonitor_L1SecondForkJudge_NotAtL1First(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1FirstThreshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
	}

	// At L1 first (10 turns), should not request fork
	if pm.ShouldForkProgressEval() {
		t.Error("L1 first should not request fork judge")
	}
}

func TestProgressMonitor_ForkDispatchedResetsOnProgress(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1SecondThreshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		if i == l1FirstThreshold-1 {
			pm.CheckAndInject()
		}
	}

	// Fork was dispatched but result hasn't arrived yet
	pm.MarkForkDispatched()
	if !pm.forkDispatched {
		t.Fatal("forkDispatched should be true after MarkForkDispatched")
	}

	// A heuristic progress signal arrives before the fork result
	pm.RecordTurn(
		[]api.ToolUseBlock{{Name: "write", Input: map[string]any{"file_path": "/workspace/out.txt"}}},
		[]api.ToolResult{{Content: "ok"}},
	)

	// forkDispatched should be reset by the progress signal
	if pm.forkDispatched {
		t.Error("forkDispatched should be false after heuristic progress reset")
	}
	if pm.ConsecutiveNoProgress() != 0 {
		t.Errorf("expected 0 consecutive after progress, got %d", pm.ConsecutiveNoProgress())
	}
}

func TestProgressMonitor_NegativeForkThenL2Escalates(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	// Drive to L1-second (20 turns) where fork fires
	for i := 0; i < l1SecondThreshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		if i == l1FirstThreshold-1 {
			pm.CheckAndInject() // consume first L1
		}
	}

	// Fork judge returns negative
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:      1,
		OutcomeMomentum:    2,
		FreshExploration:   1,
		Overall:            1.35,
		MeaningfulProgress: false,
		Reasoning:          "Agent is looping.",
	}
	pm.DrainForkResult()

	// L1 second reminder fires
	msg, terminate := pm.CheckAndInject()
	if terminate {
		t.Fatal("L1 second should not terminate")
	}
	if msg == nil {
		t.Fatal("L1 second reminder should fire after negative fork")
	}

	// Continue to L2 (25 turns)
	for i := l1SecondThreshold; i < l2Threshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd2_%d", i)
		pm.RecordTurn(failBlock, failResult)
	}

	if pm.Level() != ProgressLevelL2 {
		t.Fatalf("expected L2 after %d turns, got level %d", l2Threshold, pm.Level())
	}

	// L2 reminder fires (escalation proceeds normally)
	msg2, terminate2 := pm.CheckAndInject()
	if terminate2 {
		t.Fatal("L2 first check should not terminate")
	}
	if msg2 == nil {
		t.Fatal("L2 should inject FINAL WARNING")
	}
}

func TestProgressMonitor_L1SecondDeferredDuringFork(t *testing.T) {
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

	// Simulate the fork being dispatched (but result not yet returned)
	pm.MarkForkDispatched()

	// L1.2 reminder should NOT fire while fork is in flight
	msg, terminate := pm.CheckAndInject()
	if terminate {
		t.Fatal("L1 should not terminate while fork is in flight")
	}
	if msg != nil {
		t.Fatal("L1.2 reminder should be deferred while fork is in flight")
	}

	// Fork returns positive — DrainForkResult applies it and resets everything
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:      4,
		OutcomeMomentum:    3,
		FreshExploration:   4,
		Overall:            3.65,
		MeaningfulProgress: true,
		Reasoning:          "progressing.",
	}
	pm.DrainForkResult()

	if pm.ConsecutiveNoProgress() != 0 {
		t.Errorf("expected 0 consecutive after positive judge, got %d", pm.ConsecutiveNoProgress())
	}
	if pm.Level() != ProgressLevelNone {
		t.Errorf("expected level 0 after positive judge, got %d", pm.Level())
	}

	// Re-escalate to L1-second again
	for i := 0; i < l1SecondThreshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd2_%d", i)
		pm.RecordTurn(failBlock, failResult)
		if i == l1FirstThreshold-1 {
			pm.CheckAndInject()
		}
	}

	// Fork should NOT fire again (forkEvalFired is permanent)
	if pm.ShouldForkProgressEval() {
		t.Error("fork should not fire twice even after positive reset")
	}
}

func TestProgressMonitor_L1SecondFiresAfterNegativeFork(t *testing.T) {
	pm := NewProgressMonitor()
	failBlock := []api.ToolUseBlock{{Name: "Bash", Input: map[string]any{"command": "x"}}}
	failResult := []api.ToolResult{{Content: "error", IsError: true}}

	for i := 0; i < l1SecondThreshold; i++ {
		failBlock[0].Input["command"] = fmt.Sprintf("cmd_%d", i)
		pm.RecordTurn(failBlock, failResult)
		if i == l1FirstThreshold-1 {
			pm.CheckAndInject()
		}
	}

	// Fork dispatched but not yet returned → L1.2 deferred
	pm.MarkForkDispatched()
	msg, _ := pm.CheckAndInject()
	if msg != nil {
		t.Fatal("L1.2 should be deferred while fork in flight")
	}

	// Fork returns negative → no reset
	pm.forkResultCh <- &ProgressEvalResult{
		TaskAlignment:      1,
		OutcomeMomentum:    2,
		FreshExploration:   1,
		Overall:            1.35,
		MeaningfulProgress: false,
		Reasoning:          "looping.",
	}
	pm.DrainForkResult()

	// Now L1.2 should fire (deferred then resolved with negative)
	msg2, terminate2 := pm.CheckAndInject()
	if terminate2 {
		t.Fatal("L1.2 should not terminate")
	}
	if msg2 == nil {
		t.Fatal("L1.2 reminder should fire after negative fork resolves")
	}
}
