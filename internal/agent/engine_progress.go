package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// ProgressLevel represents the current escalation level.
type ProgressLevel int

const (
	ProgressLevelNone ProgressLevel = 0
	ProgressLevelL1   ProgressLevel = 1
	ProgressLevelL2   ProgressLevel = 2
	ProgressLevelL3   ProgressLevel = 3
)

const (
	l1Threshold        = 10
	l2Threshold        = 20
	l3Threshold        = 25
	l2MaxToolCalls     = 5
	slidingWindowSize  = 10
	slidingWindowFloor = -10
)

// TurnScore holds the computed score for a single turn.
type TurnScore struct {
	Score   int
	Signals []string
}

// ProgressMonitor tracks agent progress and triggers escalation levels.
type ProgressMonitor struct {
	consecutiveNoProgress int
	totalNoProgress       int
	level                 ProgressLevel
	l1Fired               bool
	l2Fired               bool
	l2TurnCount           int

	slidingWindow    []int
	slidingWindowPos int
	slidingFull      bool
	slidingL1Fired   bool

	previouslyReadPaths map[string]bool
	lastCommandHash     string
	lastErrorText       string
}

// NewProgressMonitor creates a new ProgressMonitor for tracking agent progress.
func NewProgressMonitor() *ProgressMonitor {
	return &ProgressMonitor{
		previouslyReadPaths: make(map[string]bool),
		slidingWindow:       make([]int, slidingWindowSize),
	}
}

// Level returns the current escalation level.
func (pm *ProgressMonitor) Level() ProgressLevel {
	return pm.level
}

// ConsecutiveNoProgress returns the current consecutive no-progress count.
func (pm *ProgressMonitor) ConsecutiveNoProgress() int {
	return pm.consecutiveNoProgress
}

// RecordTurn evaluates the tool results from a turn and updates the progress state.
// Returns the computed TurnScore.
func (pm *ProgressMonitor) RecordTurn(toolUseBlocks []api.ToolUseBlock, toolResults []api.ToolResult) TurnScore {
	score := pm.scoreTurn(toolUseBlocks, toolResults)

	pm.pushSlidingWindow(score.Score)

	if score.Score > 0 {
		pm.consecutiveNoProgress = 0
		pm.level = ProgressLevelNone
		pm.l1Fired = false
		pm.l2Fired = false
		pm.l2TurnCount = 0
	} else {
		pm.consecutiveNoProgress++
		pm.totalNoProgress++
	}

	pm.updateLevel()
	return score
}

func (pm *ProgressMonitor) scoreTurn(toolUseBlocks []api.ToolUseBlock, toolResults []api.ToolResult) TurnScore {
	ts := TurnScore{}

	for idx, tb := range toolUseBlocks {
		var result api.ToolResult
		if idx < len(toolResults) {
			result = toolResults[idx]
		}

		switch tb.Name {
		case "write", "edit", "notebook_edit":
			path := extractStringField(tb.Input, "file_path")
			if pm.isTaskOutput(path) {
				ts.Score += 3
				ts.Signals = append(ts.Signals, fmt.Sprintf("file_write:%s", path))
			} else if pm.isScratchpad(path) {
				ts.Score += 1
				ts.Signals = append(ts.Signals, "scratchpad_write")
			}

		case "Read":
			path := extractStringField(tb.Input, "file_path")
			if path != "" && !pm.previouslyReadPaths[path] {
				pm.previouslyReadPaths[path] = true
				ts.Score += 1
				ts.Signals = append(ts.Signals, fmt.Sprintf("new_read:%s", path))
			}

		case "Bash":
			cmd := extractStringField(tb.Input, "command")
			cmdHash := simpleHash(cmd)

			if result.IsError {
				ts.Score -= 1
				ts.Signals = append(ts.Signals, "command_failed")

				if pm.lastErrorText != "" && strings.Contains(result.Content, pm.lastErrorText) {
					ts.Score -= 2
					ts.Signals = append(ts.Signals, "repeated_error")
				}
				if len(result.Content) > 0 {
					pm.lastErrorText = extractErrorLine(result.Content)
				}
			} else {
				lower := strings.ToLower(result.Content)
				if containsTestPass(lower) {
					ts.Score += 3
					ts.Signals = append(ts.Signals, "test_pass")
				} else if containsBuildSuccess(lower) {
					ts.Score += 2
					ts.Signals = append(ts.Signals, "build_success")
				} else if strings.Contains(lower, "successfully installed") {
					ts.Score += 1
					ts.Signals = append(ts.Signals, "package_install")
				}
			}

			if cmdHash == pm.lastCommandHash {
				ts.Score -= 2
				ts.Signals = append(ts.Signals, "repeated_command")
			}
			pm.lastCommandHash = cmdHash
		}
	}

	return ts
}

func (pm *ProgressMonitor) updateLevel() {
	switch {
	case pm.consecutiveNoProgress >= l3Threshold:
		pm.level = ProgressLevelL3
	case pm.consecutiveNoProgress >= l2Threshold:
		pm.level = ProgressLevelL2
	case pm.consecutiveNoProgress >= l1Threshold:
		pm.level = ProgressLevelL1
	}

	if pm.slidingWindowSum() <= slidingWindowFloor && !pm.slidingL1Fired {
		if pm.level < ProgressLevelL1 {
			pm.level = ProgressLevelL1
		}
	}
}

// CheckAndInject checks the current level and returns a virtual user message
// to inject (if any), plus whether the session should terminate.
func (pm *ProgressMonitor) CheckAndInject() (msg *api.Message, terminate bool) {
	switch pm.level {
	case ProgressLevelL1:
		if !pm.l1Fired {
			pm.l1Fired = true
			if pm.slidingWindowSum() <= slidingWindowFloor {
				pm.slidingL1Fired = true
			}
			return &api.Message{
				Role:      api.RoleUser,
				Content:   "<system-reminder>\nPROGRESS CHECK: No progress in the last 10 turns. Reassess your approach or stop if blocked.\n</system-reminder>",
				IsVirtual: true,
			}, false
		}

	case ProgressLevelL2:
		if !pm.l2Fired {
			pm.l2Fired = true
			pm.l2TurnCount = 0
			return &api.Message{
				Role:      api.RoleUser,
				Content:   "<system-reminder>\nFINAL: 20 turns without progress. Write your best-effort result now and stop.\n</system-reminder>",
				IsVirtual: true,
			}, false
		}
		pm.l2TurnCount++
		if pm.l2TurnCount > l2MaxToolCalls {
			pm.level = ProgressLevelL3
			return nil, true
		}

	case ProgressLevelL3:
		return nil, true
	}

	return nil, false
}

// State returns the monitor state for inclusion in result events.
func (pm *ProgressMonitor) State() map[string]any {
	return map[string]any{
		"consecutive_no_progress": pm.consecutiveNoProgress,
		"total_no_progress":       pm.totalNoProgress,
		"level":                   int(pm.level),
	}
}

func (pm *ProgressMonitor) pushSlidingWindow(score int) {
	pm.slidingWindow[pm.slidingWindowPos] = score
	pm.slidingWindowPos = (pm.slidingWindowPos + 1) % slidingWindowSize
	if pm.slidingWindowPos == 0 {
		pm.slidingFull = true
	}
}

func (pm *ProgressMonitor) slidingWindowSum() int {
	sum := 0
	count := slidingWindowSize
	if !pm.slidingFull {
		count = pm.slidingWindowPos
	}
	for i := 0; i < count; i++ {
		sum += pm.slidingWindow[i]
	}
	return sum
}

func (pm *ProgressMonitor) isTaskOutput(path string) bool {
	if path == "" {
		return false
	}
	if pm.isScratchpad(path) {
		return false
	}
	if strings.Contains(path, "/tmp/") || strings.HasPrefix(path, "/tmp/") {
		return false
	}
	return true
}

func (pm *ProgressMonitor) isScratchpad(path string) bool {
	return strings.Contains(path, "JENNY_SCRATCHPAD") || strings.Contains(path, "jenny_scratchpad")
}

func extractStringField(input map[string]any, key string) string {
	if v, ok := input[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func simpleHash(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func extractErrorLine(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fail") {
			if len(line) > 100 {
				return line[:100]
			}
			return line
		}
	}
	if len(content) > 100 {
		return content[:100]
	}
	return content
}

func containsTestPass(lower string) bool {
	patterns := []string{" pass ", " pass:", " passed ", " ok ", " success ", " success:", "tests passed", "pass\n", "success\n"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// terminateProgressMonitor handles L3 hard termination.
func (e *QueryEngine) terminateProgressMonitor(sessionID string) (string, error) {
	state := e.progressMonitor.State()
	result := fmt.Sprintf("terminated: no progress for %d consecutive turns", state["consecutive_no_progress"])

	if e.streamCfg.Enabled {
		msg := StreamMessage{
			Type:          "result",
			Subtype:       "progress_terminated",
			Result:        result,
			SessionID:     sessionID,
			Uuid:          GenerateUUID(),
			Model:         e.model,
			IsError:       true,
			StopReason:    "progress_terminated",
			DurationMs:    time.Since(e.startTime).Milliseconds(),
			DurationAPIMs: e.totalAPIDurationMs,
			TotalCostUSD:  e.costState.TotalCostUSD,
			ModelUsage:    e.buildModelUsage(),
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}

	return "", fmt.Errorf("error_progress_terminated: %s", result)
}

func containsBuildSuccess(lower string) bool {
	patterns := []string{"build successful", "compiled", "compilation succeeded"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
