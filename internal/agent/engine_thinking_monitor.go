package agent

import (
	"fmt"

	"github.com/ipy/jenny/internal/log"
)

// ThinkingMonitor tracks per-turn thinking token usage and determines
// when to inject a system reminder to encourage less thinking and more action.
type ThinkingMonitor struct {
	history          []turnThinkingRecord
	reminderInjected int // number of reminders injected so far
	maxReminders     int // cap to avoid spamming
}

type turnThinkingRecord struct {
	Turn           int
	ThinkingTokens int
	HadToolCall    bool
	Complexity     float64 // OverallComplexity at the time, or 3.0 default
}

// NewThinkingMonitor creates a new monitor.
func NewThinkingMonitor() *ThinkingMonitor {
	return &ThinkingMonitor{
		maxReminders: 3,
	}
}

const maxThinkingHistory = 20

// RecordTurn records the thinking tokens and tool usage for a completed turn.
func (tm *ThinkingMonitor) RecordTurn(turn, thinkingTokens int, hadToolCall bool, complexity float64) {
	if complexity < 1.0 {
		complexity = 3.0
	}
	tm.history = append(tm.history, turnThinkingRecord{
		Turn:           turn,
		ThinkingTokens: thinkingTokens,
		HadToolCall:    hadToolCall,
		Complexity:     complexity,
	})
	if len(tm.history) > maxThinkingHistory {
		tm.history = tm.history[len(tm.history)-maxThinkingHistory:]
	}
}

// ShouldRemind evaluates whether a system reminder should be injected.
// Returns the reminder text (empty if no reminder needed).
func (tm *ThinkingMonitor) ShouldRemind() string {
	if tm.reminderInjected >= tm.maxReminders {
		return ""
	}
	if len(tm.history) < 2 {
		return ""
	}

	score := tm.computeScore()
	log.Debug("ThinkingMonitor score", "score", fmt.Sprintf("%.2f", score), "turns", len(tm.history))

	if score >= 0.7 {
		tm.reminderInjected++
		return thinkingReminderText
	}
	return ""
}

// computeScore returns a 0-1 score indicating how "thinking-heavy" recent behavior is.
// Higher = more thinking, less action. Dimensions:
//
//  1. Token/complexity ratio: thinking_tokens / (complexity * 2000) — are we thinking
//     disproportionately to task difficulty?
//  2. Consecutive no-tool turns: how many recent turns had no tool calls?
//  3. Early turn penalty: the first 3 turns should have low thinking.
//  4. Trend: is thinking increasing over the last few turns?
func (tm *ThinkingMonitor) computeScore() float64 {
	n := len(tm.history)
	if n == 0 {
		return 0
	}

	// Look at last 5 turns (or fewer)
	window := 5
	if n < window {
		window = n
	}
	recent := tm.history[n-window:]

	// Dimension 1: Token/complexity ratio for the latest turn
	latest := recent[len(recent)-1]
	expectedTokens := latest.Complexity * 2000
	if expectedTokens < 2000 {
		expectedTokens = 2000
	}
	ratioScore := float64(latest.ThinkingTokens) / expectedTokens
	if ratioScore > 1.0 {
		ratioScore = 1.0
	}

	// Dimension 2: Consecutive no-tool-call turns (from the end)
	noToolCount := 0
	for i := len(recent) - 1; i >= 0; i-- {
		if !recent[i].HadToolCall {
			noToolCount++
		} else {
			break
		}
	}
	noToolScore := float64(noToolCount) / float64(window)
	if noToolScore > 1.0 {
		noToolScore = 1.0
	}

	// Dimension 3: Contextual penalty — pick the strongest applicable penalty, not additive
	earlyPenalty := 0.0
	if latest.Turn <= 3 && latest.ThinkingTokens > 3000 {
		earlyPenalty = 0.3
	}
	if latest.Complexity < 2.0 && ratioScore >= 0.85 {
		if 0.35 > earlyPenalty {
			earlyPenalty = 0.35
		}
	}

	// Dimension 4: Trend — is thinking increasing?
	trendScore := 0.0
	if len(recent) >= 3 {
		firstHalf := 0
		secondHalf := 0
		mid := len(recent) / 2
		for i := 0; i < mid; i++ {
			firstHalf += recent[i].ThinkingTokens
		}
		for i := mid; i < len(recent); i++ {
			secondHalf += recent[i].ThinkingTokens
		}
		if firstHalf > 0 {
			increase := float64(secondHalf-firstHalf) / float64(firstHalf)
			if increase > 0 {
				trendScore = increase
				if trendScore > 1.0 {
					trendScore = 1.0
				}
			}
		}
	}

	// Weighted combination
	score := ratioScore*0.35 + noToolScore*0.30 + earlyPenalty + trendScore*0.15

	// Reduce if fewer than 3 data points (too early to judge reliably)
	if n < 3 {
		score *= 0.5
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

const thinkingReminderText = `<system-reminder>
You are spending too many tokens on internal reasoning relative to actual progress. Prioritize action over analysis:

- Execute tool calls immediately rather than deliberating further
- If you have a reasonable hypothesis, test it with a tool call instead of reasoning about alternatives
- Limit planning to 2-3 sentences before your next action
- When reading code, focus on the specific section you need rather than analyzing the entire file

Act first, analyze results after.
</system-reminder>`
