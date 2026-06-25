package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
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
	l1FirstThreshold  = 10
	l1SecondThreshold = 20
	l2Threshold       = 25
	l3Threshold       = 30
	l2MaxTurns        = 5
	slidingWindowSize = 10
	// Sliding window can only trigger L1 (not L2/L3).
	slidingWindowFloor = -10
	// Cap for previouslyReadPaths to prevent unbounded growth.
	maxTrackedReadPaths = 200
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
	l1FirstFired          bool
	l1SecondFired         bool
	l2Fired               bool
	l2TurnCount           int

	slidingWindow    []int
	slidingWindowPos int
	slidingFull      bool
	slidingL1Fired   bool

	previouslyReadPaths    map[string]bool
	readPathOrder          []string       // FIFO insertion order for eviction
	previouslyWrittenPaths map[string]int // path → write count
	lastCommandHash        string
	lastErrorText          string

	// L2 fork-judge state
	forkDispatched bool
	forkResultCh   chan *ProgressEvalResult

	// L3 fork-judge state: before hard termination, dispatch one final fork-judge.
	// If the judge says meaningful progress, reset counters.
	l3ForkDispatched bool
}

// ProgressEvalResult holds the LLM judge's evaluation of agent progress.
type ProgressEvalResult struct {
	TaskAlignment      int     `json:"task_alignment"`
	OutcomeMomentum    int     `json:"outcome_momentum"`
	FreshExploration   int     `json:"fresh_exploration"`
	Overall            float64 `json:"overall"`
	MeaningfulProgress bool    `json:"meaningful_progress"`
	Reasoning          string  `json:"reasoning"`
}

// NewProgressMonitor creates a new ProgressMonitor for tracking agent progress.
func NewProgressMonitor() *ProgressMonitor {
	return &ProgressMonitor{
		previouslyReadPaths:    make(map[string]bool),
		previouslyWrittenPaths: make(map[string]int),
		slidingWindow:          make([]int, slidingWindowSize),
		forkResultCh:           make(chan *ProgressEvalResult, 1),
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

// resetScoreForLevel returns the minimum positive score needed to reset the
// counter at a given escalation level. The thresholds are kept low so that
// genuine recovery signals (a new file write, a passing test) can break out
// of escalation at any level. A ratchet effect (ever-increasing thresholds)
// makes recovery nearly impossible for agents that are making partial progress.
//
//	L0 (None): score >= 1 (any positive signal)
//	L1:        score >= 1 (L1 is an advisory reminder, same bar as L0)
//	L2:        score >= 2 (moderate signal: a file write or build success)
//	L3:        score >= 2 (last chance — same bar as L2 to allow rescue)
func resetScoreForLevel(level ProgressLevel) int {
	switch level {
	case ProgressLevelL3:
		return 2
	case ProgressLevelL2:
		return 2
	case ProgressLevelL1:
		return 1
	default:
		return 1
	}
}

// RecordTurn evaluates the tool results from a turn and updates the progress state.
// Returns the computed TurnScore.
func (pm *ProgressMonitor) RecordTurn(toolUseBlocks []api.ToolUseBlock, toolResults []api.ToolResult) TurnScore {
	score := pm.scoreTurn(toolUseBlocks, toolResults)

	pm.pushSlidingWindow(score.Score)

	requiredScore := resetScoreForLevel(pm.level)
	if score.Score >= requiredScore {
		pm.consecutiveNoProgress = 0
		pm.level = ProgressLevelNone
		pm.l1FirstFired = false
		pm.l1SecondFired = false
		pm.l2Fired = false
		pm.l2TurnCount = 0
		pm.forkDispatched = false
		pm.l3ForkDispatched = false
	} else {
		if score.Score <= 0 {
			pm.consecutiveNoProgress++
			pm.totalNoProgress++
		}
	}

	prevLevel := pm.level
	pm.updateLevel()

	log.Debug("Progress monitor turn",
		"score", score.Score,
		"signals", strings.Join(score.Signals, ","),
		"consecutive_no_progress", pm.consecutiveNoProgress,
		"level", int(pm.level),
		"sliding_sum", pm.slidingWindowSum(),
		"required_score", requiredScore,
	)

	if pm.level != prevLevel {
		log.Info("Progress monitor level changed",
			"from", int(prevLevel),
			"to", int(pm.level),
			"consecutive_no_progress", pm.consecutiveNoProgress,
		)
	}

	return score
}

// Known test runner command prefixes. The command (lowered, trimmed) is checked
// against these prefixes with word-boundary enforcement. When a match is found
// AND IsError == false (exit 0), the turn is scored as test_pass (+3).
var testRunnerPrefixes = []string{
	"go test",
	"cargo test",
	"cargo nextest",
	"pytest",
	"python -m pytest",
	"python3 -m pytest",
	"python -m unittest",
	"python3 -m unittest",
	"npm test",
	"npm run test",
	"npx jest",
	"npx vitest",
	"npx mocha",
	"yarn test",
	"pnpm test",
	"make test",
	"make check",
	"ctest",
	"dotnet test",
	"mvn test",
	"mvn verify",
	"gradle test",
	"gradlew test",
	"./gradlew test",
	"rspec",
	"mix test",
	"bats",
	"phpunit",
	"./vendor/bin/phpunit",
	"tox",
	"nose2",
}

// Known build command prefixes. Exit 0 → build_success (+2).
// Word-boundary enforcement prevents "makefile" from matching "make".
var buildRunnerPrefixes = []string{
	"make",
	"go build",
	"cargo build",
	"gcc",
	"g++",
	"cc",
	"c++",
	"clang",
	"clang++",
	"cmake --build",
	"ninja",
	"dotnet build",
	"dotnet publish",
	"mvn compile",
	"mvn package",
	"gradle build",
	"gradlew build",
	"./gradlew build",
	"javac",
	"rustc",
	"./configure",
	"bazel build",
	"buck build",
	"msbuild",
	"xcodebuild",
	"sbt compile",
	"sbt package",
}

// Known package install prefixes. Exit 0 → package_install (+1).
var packageInstallPrefixes = []string{
	"apt-get install",
	"apt install",
	"apt-get update",
	"pip install",
	"pip3 install",
	"python -m pip install",
	"python3 -m pip install",
	"npm install",
	"npm ci",
	"yarn install",
	"yarn add",
	"pnpm install",
	"pnpm add",
	"cargo install",
	"go install",
	"go get",
	"gem install",
	"bundle install",
	"brew install",
	"dnf install",
	"yum install",
	"pacman -s",
	"apk add",
	"conda install",
	"pipx install",
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
				pm.previouslyWrittenPaths[path]++
				count := pm.previouslyWrittenPaths[path]
				score, signal := fileWriteScore(count)
				ts.Score += score
				ts.Signals = append(ts.Signals, fmt.Sprintf("%s:%s", signal, path))
			} else if pm.isScratchpad(path) {
				ts.Score += 1
				ts.Signals = append(ts.Signals, "scratchpad_write")
			}

		case "Read":
			path := extractStringField(tb.Input, "file_path")
			if path != "" && !pm.previouslyReadPaths[path] {
				for len(pm.previouslyReadPaths) >= maxTrackedReadPaths {
					pm.evictOldestReadPath()
				}
				pm.previouslyReadPaths[path] = true
				pm.readPathOrder = append(pm.readPathOrder, path)
				ts.Score += 1
				ts.Signals = append(ts.Signals, fmt.Sprintf("new_read:%s", path))
			}

		case "Bash":
			cmd := extractStringField(tb.Input, "command")
			cmdHash := simpleHash(cmd)
			cmdLower := strings.ToLower(strings.TrimSpace(cmd))

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
				// Structured signal: exit code 0 + known runner prefix.
				if matchesPrefix(cmdLower, testRunnerPrefixes) {
					ts.Score += 3
					ts.Signals = append(ts.Signals, "test_pass")
				} else if matchesPrefix(cmdLower, buildRunnerPrefixes) {
					ts.Score += 2
					ts.Signals = append(ts.Signals, "build_success")
				} else if matchesPrefix(cmdLower, packageInstallPrefixes) {
					ts.Score += 1
					ts.Signals = append(ts.Signals, "package_install")
				} else if hasPackageInstallOutput(result.Content) {
					ts.Score += 1
					ts.Signals = append(ts.Signals, "package_install_detected")
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

// matchesPrefix checks if cmd starts with any of the given prefixes,
// requiring a word boundary after the prefix. For compound commands
// (joined by &&, ||, or ;), each segment is tested independently.
func matchesPrefix(cmdLower string, prefixes []string) bool {
	segments := splitCompoundCommand(cmdLower)
	for _, seg := range segments {
		if matchesSinglePrefix(seg, prefixes) {
			return true
		}
	}
	return false
}

// splitCompoundCommand splits a shell command by &&, ||, and ;
// returning the trimmed segments.
func splitCompoundCommand(cmd string) []string {
	// Replace || and && with a unique delimiter first, then split by ;
	// We split manually to handle && vs || correctly.
	var segments []string
	remaining := cmd
	for remaining != "" {
		idx := -1
		sep := ""
		for _, s := range []string{"&&", "||", ";"} {
			if i := strings.Index(remaining, s); i >= 0 && (idx < 0 || i < idx) {
				idx = i
				sep = s
			}
		}
		if idx < 0 {
			segments = append(segments, strings.TrimSpace(remaining))
			break
		}
		seg := strings.TrimSpace(remaining[:idx])
		if seg != "" {
			segments = append(segments, seg)
		}
		remaining = remaining[idx+len(sep):]
	}
	return segments
}

// matchesSinglePrefix checks a single (non-compound) command against prefixes.
func matchesSinglePrefix(cmdLower string, prefixes []string) bool {
	for _, p := range prefixes {
		if !strings.HasPrefix(cmdLower, p) {
			continue
		}
		if len(cmdLower) == len(p) {
			return true
		}
		next := cmdLower[len(p)]
		if next == ' ' || next == '\t' || next == '/' || next == '=' || next == '-' {
			return true
		}
	}
	return false
}

// hasPackageInstallOutput detects package installation via output patterns.
// Used as fallback when the command itself isn't a known installer prefix
// (e.g., piped or wrapped install commands).
func hasPackageInstallOutput(output string) bool {
	lines := strings.Split(output, "\n")
	signals := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Setting up "):
			signals++
		case strings.HasPrefix(trimmed, "Successfully installed "):
			return true
		case strings.Contains(trimmed, "added") && strings.Contains(trimmed, "packages"):
			return true
		case strings.HasPrefix(trimmed, "Compiling ") && strings.Contains(trimmed, " v"):
			signals++
		}
		if signals >= 3 {
			return true
		}
	}
	return false
}

func (pm *ProgressMonitor) updateLevel() {
	// Consecutive no-progress counter drives L1 → L2 → L3 escalation.
	switch {
	case pm.consecutiveNoProgress >= l3Threshold:
		pm.level = ProgressLevelL3
	case pm.consecutiveNoProgress >= l2Threshold:
		pm.level = ProgressLevelL2
	case pm.consecutiveNoProgress >= l1FirstThreshold:
		pm.level = ProgressLevelL1
	}

	// Sliding window can only trigger L1, never L2 or L3.
	if pm.slidingWindowSum() <= slidingWindowFloor && !pm.slidingL1Fired {
		if pm.level < ProgressLevelL1 {
			pm.level = ProgressLevelL1
		}
	}
}

// CheckAndInject checks the current level and returns a virtual user message
// to inject (if any), plus whether the session should terminate.
//
// L1 fires twice: once at l1FirstThreshold (10) and again at l1SecondThreshold (20).
// L2 fires at l2Threshold (25) as a soft terminate with l2MaxTurns (5) grace.
// L3 fires at l3Threshold (30) as a hard terminate.
func (pm *ProgressMonitor) CheckAndInject() (msg *api.Message, terminate bool) {
	switch pm.level {
	case ProgressLevelL1:
		if pm.consecutiveNoProgress >= l1SecondThreshold && !pm.l1SecondFired {
			// If a fork is in flight, defer the L1.2 reminder until the result arrives.
			if pm.forkDispatched {
				return nil, false
			}
			pm.l1SecondFired = true
			return &api.Message{
				Role:      api.RoleUser,
				Content:   l1SecondReminderText,
				IsVirtual: true,
			}, false
		}
		if !pm.l1FirstFired {
			pm.l1FirstFired = true
			if pm.slidingWindowSum() <= slidingWindowFloor {
				pm.slidingL1Fired = true
			}
			return &api.Message{
				Role:      api.RoleUser,
				Content:   l1FirstReminderText,
				IsVirtual: true,
			}, false
		}

	case ProgressLevelL2:
		if !pm.l2Fired {
			pm.l2Fired = true
			pm.l2TurnCount = 0
			return &api.Message{
				Role:      api.RoleUser,
				Content:   l2ReminderText,
				IsVirtual: true,
			}, false
		}
		pm.l2TurnCount++
		if pm.l2TurnCount > l2MaxTurns {
			pm.level = ProgressLevelL3
			// Fall through to L3 handler (which may dispatch a fork-judge
			// before terminating) on the next CheckAndInject call.
			return nil, false
		}

	case ProgressLevelL3:
		if pm.forkDispatched {
			// L3 fork-judge is in flight; defer termination until result arrives.
			return nil, false
		}
		if pm.l3ForkDispatched {
			// Fork completed with negative result (level still L3) → hard terminate.
			return nil, true
		}
		// No fork dispatched yet — the main loop's ShouldForkL3ProgressEval
		// will fire on the same iteration; defer termination for one cycle.
		return nil, true
	}

	return nil, false
}

const l1FirstReminderText = `<system-reminder>
PROGRESS CHECK: Limited progress detected over recent turns. Consider:

1. CAPABILITY: Does this task require abilities you may lack (vision, specific runtime, etc.)? If so, it may be best to write a best-effort output and stop.
2. STRATEGY: If your current approach has not been converging, consider pivoting to a substantively different strategy rather than retrying the same approach.

Continue with your current approach if you believe it is working, or pivot if you see a better path forward.
</system-reminder>`

const l1SecondReminderText = `<system-reminder>
PROGRESS CHECK (2nd): Extended period without clear progress. Be aware that continued lack of progress may result in session termination.

Consider whether a different strategy might be more effective. If you believe you are close to a solution, continue — but avoid repeating approaches that have already failed.

If you are genuinely stuck, writing a partial or best-effort result is preferable to no output.
</system-reminder>`

const l2ReminderText = `<system-reminder>
PROGRESS WARNING: You have a limited number of turns remaining before automatic termination.

Suggestions:
- If you have a working partial result, ensure it is saved to the expected output location.
- If you have an untried strategy, now is the time to attempt it.
- A partial answer is better than no answer.
</system-reminder>`

// State returns the monitor state for inclusion in result events.
func (pm *ProgressMonitor) State() map[string]any {
	return map[string]any{
		"consecutive_no_progress": pm.consecutiveNoProgress,
		"total_no_progress":       pm.totalNoProgress,
		"level":                   int(pm.level),
	}
}

// ShouldForkProgressEval returns true when the L1-second fork-judge should be dispatched.
// It fires exactly once per L1-second trigger — when the consecutive no-progress count
// reaches l1SecondThreshold (20) and the second L1 reminder hasn't fired yet.
func (pm *ProgressMonitor) ShouldForkProgressEval() bool {
	return pm.consecutiveNoProgress >= l1SecondThreshold && !pm.l1SecondFired && !pm.forkDispatched
}

// MarkForkDispatched records that the fork has been launched.
func (pm *ProgressMonitor) MarkForkDispatched() {
	pm.forkDispatched = true
}

// ShouldForkL3ProgressEval returns true when the L3 fork-judge should be dispatched.
// It fires exactly once when L3 is reached (hard termination imminent) to give the
// agent one last chance: if the judge finds meaningful progress, the session is "rescued".
func (pm *ProgressMonitor) ShouldForkL3ProgressEval() bool {
	return pm.level >= ProgressLevelL3 && !pm.l3ForkDispatched && !pm.forkDispatched
}

// MarkL3ForkDispatched records that the L3 fork has been launched.
func (pm *ProgressMonitor) MarkL3ForkDispatched() {
	pm.l3ForkDispatched = true
	pm.forkDispatched = true
}

// forkJudgeThreshold is the fixed threshold for fork-judge evaluations.
// Using a constant avoids penalizing later evaluations: L3 is the agent's
// last chance and should not face a higher bar than earlier checks.
const forkJudgeThreshold = 3.0

// ApplyProgressEvalResult applies the judge result. If meaningful progress is found
// (MeaningfulProgress was already computed by parseProgressEvalResponse), all
// counters and escalation state reset to 0. Otherwise the L1-second flow continues.
// Always clears forkDispatched — the fork has completed and delivered its result.
func (pm *ProgressMonitor) ApplyProgressEvalResult(result *ProgressEvalResult) {
	pm.forkDispatched = false

	log.Debug("Fork-judge result applied",
		"threshold", forkJudgeThreshold,
		"overall", result.Overall,
		"meaningful", result.MeaningfulProgress,
	)

	if result.MeaningfulProgress {
		pm.consecutiveNoProgress = 0
		pm.level = ProgressLevelNone
		pm.l1FirstFired = false
		pm.l1SecondFired = false
		pm.l2Fired = false
		pm.l2TurnCount = 0
		pm.slidingL1Fired = false
		pm.forkDispatched = false
		pm.l3ForkDispatched = false
		return
	}
	// Negative result: let the escalation flow continue normally.
}

// DrainForkResult non-blockingly drains the fork result channel and applies
// the result. Called from the main loop before CheckAndInject.
func (pm *ProgressMonitor) DrainForkResult() {
	select {
	case result := <-pm.forkResultCh:
		pm.ApplyProgressEvalResult(result)
	default:
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

// fileWriteScore returns the score and signal name for the nth write to the same file.
//
//	1st write: +3 (file_write)    — fresh output, strong signal
//	2nd write: +1 (file_rewrite)  — iteration, mild positive
//	3rd write:  0 (rewrite_neutral) — diminishing returns
//	4th+ write: -1 (rewrite_stale) — spinning, capped at -1 to avoid over-punishment
func fileWriteScore(writeCount int) (int, string) {
	switch {
	case writeCount <= 1:
		return 3, "file_write"
	case writeCount == 2:
		return 1, "file_rewrite"
	case writeCount == 3:
		return 0, "rewrite_neutral"
	default:
		return -1, "rewrite_stale"
	}
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

// evictOldestReadPath removes the oldest entry (FIFO) from previouslyReadPaths.
func (pm *ProgressMonitor) evictOldestReadPath() {
	if len(pm.readPathOrder) == 0 {
		return
	}
	oldest := pm.readPathOrder[0]
	copy(pm.readPathOrder, pm.readPathOrder[1:])
	pm.readPathOrder = pm.readPathOrder[:len(pm.readPathOrder)-1]
	delete(pm.previouslyReadPaths, oldest)
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

const progressEvalPrompt = `<system-reminder>
This is a separate meta-evaluation request. Prior conversation is from another agent, do not continue it. TOOL CALLS ARE FORBIDDEN.
</system-reminder>
Evaluate whether the previous conversation is making genuine forward progress. Score 1-5 on each dimension:

1. TASK_PROGRESS (1-5): Is the agent's approach actually advancing the user's goal, or is it stuck performing repeated actions that won't converge to a solution? "Working hard" on a task the agent lacks the capability to complete (wrong domain, missing data, blocked by an external dependency) is LOW. Genuinely moving toward a deliverable is HIGH. Mere execution of commands without resolution is LOW.
2. OUTCOME_MOMENTUM (1-5): Are the agent's intermediate outputs converging? Are errors being resolved over time? Are the latest results closer to the goal than earlier ones? Score HIGH for clear incremental gains, LOW for stagnant or oscillating outputs.
3. FRESH_EXPLORATION (1-5): Is each turn probing something new, or is the agent cycling through the same approaches, files, or commands? Score LOW if the agent keeps retrying failed approaches.

Respond ONLY with a JSON object, no other text, comments, or fences:
{"task_alignment": <1-5>, "outcome_momentum": <1-5>, "fresh_exploration": <1-5>, "overall": <weighted average, 2 decimal places>, "meaningful_progress": <true if overall >= 3.0, false otherwise>, "reasoning": "<one sentence>"}
Weights: task_alignment 50%%, outcome_momentum 35%%, fresh_exploration 15%%.`

// forkProgressEval sends a side request appending the judge prompt to a copy
// of the conversation history. The shared prefix (system prompt + prior messages)
// enables prompt cache hits.
func forkProgressEval(ctx context.Context, client api.Requester, messages []api.Message, systemPrompt []string, tools []api.ToolParam) (*ProgressEvalResult, string, api.Usage, error) {
	evalMsg := api.Message{
		Role:    api.RoleUser,
		Content: progressEvalPrompt,
	}
	forkMessages := make([]api.Message, len(messages), len(messages)+1)
	copy(forkMessages, messages)
	forkMessages = append(forkMessages, evalMsg)

	resp, err := client.SendMessage(ctx, forkMessages, tools, nil, systemPrompt, "")
	if err != nil {
		return nil, "", api.Usage{}, err
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" || block.Text != "" {
			text.WriteString(block.Text)
		}
	}

	result, err := parseProgressEvalResponse(text.String())
	if err != nil {
		return nil, resp.Model, resp.Usage, err
	}

	log.Debug("Progress eval result",
		"task_alignment", result.TaskAlignment,
		"outcome_momentum", result.OutcomeMomentum,
		"fresh_exploration", result.FreshExploration,
		"overall", result.Overall,
		"meaningful_progress", result.MeaningfulProgress,
	)

	return result, resp.Model, resp.Usage, nil
}

// progressEvalResultJSON marshals a ProgressEvalResult to a JSON string for stream events.
func progressEvalResultJSON(result *ProgressEvalResult) string {
	data, _ := json.Marshal(result)
	return string(data)
}

// parseProgressEvalResponse extracts and parses the JSON response from the judge.
func parseProgressEvalResponse(text string) (*ProgressEvalResult, error) {
	text = extractJSON(text)

	var result ProgressEvalResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, err
	}

	if result.TaskAlignment < 1 || result.TaskAlignment > 5 {
		return nil, fmt.Errorf("task_alignment out of range: %d", result.TaskAlignment)
	}
	if result.OutcomeMomentum < 1 || result.OutcomeMomentum > 5 {
		return nil, fmt.Errorf("outcome_momentum out of range: %d", result.OutcomeMomentum)
	}
	if result.FreshExploration < 1 || result.FreshExploration > 5 {
		return nil, fmt.Errorf("fresh_exploration out of range: %d", result.FreshExploration)
	}

	// Compute overall from weighted average (ignore the model's overall to avoid gaming).
	overall := float64(result.TaskAlignment)*0.50 + float64(result.OutcomeMomentum)*0.35 + float64(result.FreshExploration)*0.15
	result.Overall = overall
	result.MeaningfulProgress = overall >= forkJudgeThreshold

	return &result, nil
}
