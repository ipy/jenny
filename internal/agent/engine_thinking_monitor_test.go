package agent

import (
	"testing"
)

func TestThinkingMonitor_NoReminderEarly(t *testing.T) {
	tm := NewThinkingMonitor()
	tm.RecordTurn(1, 1000, true, 3.0)
	if r := tm.ShouldRemind(); r != "" {
		t.Error("should not remind after only 1 turn")
	}
}

func TestThinkingMonitor_ReminderOnExcessiveThinking(t *testing.T) {
	tm := NewThinkingMonitor()
	// Simulate several turns with high thinking and no tool calls
	for i := 1; i <= 5; i++ {
		tm.RecordTurn(i+3, 8000, false, 2.0)
	}
	r := tm.ShouldRemind()
	if r == "" {
		t.Error("expected reminder for excessive thinking with no tool calls")
	}
}

func TestThinkingMonitor_NoReminderWithToolCalls(t *testing.T) {
	tm := NewThinkingMonitor()
	for i := 1; i <= 5; i++ {
		tm.RecordTurn(i+3, 3000, true, 3.0)
	}
	r := tm.ShouldRemind()
	if r != "" {
		t.Errorf("should not remind when tool calls are being made, got: %s", r[:50])
	}
}

func TestThinkingMonitor_MaxReminders(t *testing.T) {
	tm := NewThinkingMonitor()
	count := 0
	for cycle := 0; cycle < 10; cycle++ {
		for i := 0; i < 5; i++ {
			tm.RecordTurn(cycle*5+i+4, 10000, false, 1.0)
		}
		if r := tm.ShouldRemind(); r != "" {
			count++
		}
	}
	if count > tm.maxReminders {
		t.Errorf("expected at most %d reminders, got %d", tm.maxReminders, count)
	}
}

func TestThinkingMonitor_EarlyTurnPenalty(t *testing.T) {
	tm := NewThinkingMonitor()
	// High thinking in early turns (turn 1-3)
	tm.RecordTurn(1, 5000, false, 2.0)
	tm.RecordTurn(2, 6000, false, 2.0)
	tm.RecordTurn(3, 7000, false, 2.0)
	r := tm.ShouldRemind()
	if r == "" {
		t.Error("expected reminder for excessive thinking in early turns")
	}
}

func TestThinkingMonitor_LowComplexityHighThinking(t *testing.T) {
	tm := NewThinkingMonitor()
	// Low complexity task but lots of thinking
	for i := 1; i <= 4; i++ {
		tm.RecordTurn(i+3, 7000, true, 1.5)
	}
	r := tm.ShouldRemind()
	if r == "" {
		t.Error("expected reminder for high thinking on low-complexity task")
	}
}
