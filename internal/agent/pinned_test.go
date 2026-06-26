package agent

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/session"
)

func TestBuildCompactedChain_BoundaryIsPinned(t *testing.T) {
	messages := []api.Message{
		{Role: api.RoleUser, Content: "task", Pinned: true},
		{Role: api.RoleUser, Content: "follow-up"},
	}
	result := buildCompactedChain(messages, "summary text", 100_000)
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if !result[0].Pinned {
		t.Error("compaction boundary message should be pinned")
	}
	if !isCompactionBoundary(result[0]) {
		t.Error("first message should be compaction boundary")
	}
}

func TestBuildCompactedChain_PreservesPinnedMessages(t *testing.T) {
	messages := make([]api.Message, 15)
	messages[0] = api.Message{Role: api.RoleUser, Content: "original task", Pinned: true}
	for i := 1; i < 15; i++ {
		messages[i] = api.Message{Role: api.RoleUser, Content: "filler message"}
	}

	result := buildCompactedChain(messages, "summary", 100_000)
	if !result[0].Pinned {
		t.Fatal("boundary should be pinned")
	}

	foundOriginal := false
	for _, msg := range result {
		if msg.Content == "original task" && msg.Pinned {
			foundOriginal = true
			break
		}
	}
	if !foundOriginal {
		t.Error("pinned original task should survive compaction")
	}
}

func TestBuildCompactedChain_OrderBoundaryPinnedTail(t *testing.T) {
	messages := make([]api.Message, 15)
	messages[0] = api.Message{Role: api.RoleUser, Content: "pinned-task", Pinned: true}
	messages[5] = api.Message{Role: api.RoleUser, Content: "pinned-constraint", Pinned: true}
	for i := range messages {
		if messages[i].Content == "" {
			messages[i] = api.Message{Role: api.RoleUser, Content: "filler"}
		}
	}
	messages[14] = api.Message{Role: api.RoleUser, Content: "recent-tail"}

	result := buildCompactedChain(messages, "summary", 100_000)

	if !isCompactionBoundary(result[0]) {
		t.Fatalf("index 0 should be boundary, got %q", result[0].Content)
	}

	// Find indices of pinned-task and pinned-constraint — must appear before recent-tail
	taskIdx, constraintIdx, tailIdx := -1, -1, -1
	for i, msg := range result {
		switch msg.Content {
		case "pinned-task":
			taskIdx = i
		case "pinned-constraint":
			constraintIdx = i
		case "recent-tail":
			tailIdx = i
		}
	}
	if taskIdx < 0 || constraintIdx < 0 || tailIdx < 0 {
		t.Fatalf("missing expected messages in result: task=%d constraint=%d tail=%d", taskIdx, constraintIdx, tailIdx)
	}
	if !(taskIdx < tailIdx && constraintIdx < tailIdx && taskIdx < constraintIdx) {
		t.Errorf("expected order boundary → pinned-task → pinned-constraint → tail; indices task=%d constraint=%d tail=%d", taskIdx, constraintIdx, tailIdx)
	}
}

func TestEnforcePinnedLimit_UnpinsOldestNonBoundary(t *testing.T) {
	msgs := make([]api.Message, 0, maxPinnedMessages+2)
	msgs = append(msgs, api.Message{Role: api.RoleUser, Content: "boundary", Pinned: true})
	for i := 0; i < maxPinnedMessages+1; i++ {
		msgs = append(msgs, api.Message{
			Role:    api.RoleUser,
			Content: "pinned-" + string(rune('a'+i)),
			Pinned:  true,
		})
	}
	// Mark first as boundary for limit logic
	msgs[0] = api.Message{
		Role:    api.RoleUser,
		Content: "<system-reminder>\n" + compactSummaryPrefix + "summary\n</system-reminder>",
		Pinned:  true,
	}

	result := enforcePinnedLimit(msgs)
	pinnedCount := 0
	for _, msg := range result {
		if msg.Pinned {
			pinnedCount++
		}
	}
	if pinnedCount > maxPinnedMessages {
		t.Errorf("pinned count = %d, want <= %d", pinnedCount, maxPinnedMessages)
	}
	// Oldest non-boundary (pinned-a) should be unpinned
	for _, msg := range result {
		if msg.Content == "pinned-a" && msg.Pinned {
			t.Error("oldest non-boundary pinned message should have been unpinned")
		}
	}
}

func TestIsCompactionBoundary(t *testing.T) {
	boundary := api.Message{
		Role:    api.RoleUser,
		Content: "<system-reminder>\n" + compactSummaryPrefix + "summary\n</system-reminder>",
	}
	if !isCompactionBoundary(boundary) {
		t.Error("expected compaction boundary detection")
	}
	plain := api.Message{Role: api.RoleUser, Content: "hello"}
	if isCompactionBoundary(plain) {
		t.Error("plain user message should not be boundary")
	}
}

func TestFormatPinnedSummaryNote(t *testing.T) {
	pinned := []api.Message{
		{Role: api.RoleUser, Content: "Build a REST API with tests", Pinned: true},
		{Role: api.RoleUser, Content: "<system-reminder>\n" + compactSummaryPrefix + "prior summary\n</system-reminder>", Pinned: true},
	}
	note := formatPinnedSummaryNote(pinned)
	if !strings.Contains(note, "pinned and preserved verbatim") {
		t.Error("note should mention pinned messages are preserved")
	}
	if !strings.Contains(note, "Build a REST API") {
		t.Error("note should reference pinned message content")
	}
}

func TestRebuildMessages_RestoresPinned(t *testing.T) {
	entries := []session.TranscriptEntry{
		{Type: session.EntryTypeUser, Content: "original task", Pinned: true},
		{Type: session.EntryTypeAssistant, Content: "working on it"},
		{
			Type:    session.EntryTypeSystem,
			Subtype: session.SubtypeCompactBoundary,
			Pinned:  true,
			CompactMetadata: &session.CompactMetadata{
				Summary: "prior context summary",
			},
		},
	}
	msgs := RebuildMessages(entries)
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if !msgs[0].Pinned {
		t.Error("first user message should restore pinned=true")
	}
	// Boundary is last system entry in chain
	foundBoundaryPinned := false
	for _, msg := range msgs {
		if strings.Contains(msg.Content, compactSummaryPrefix) && msg.Pinned {
			foundBoundaryPinned = true
		}
	}
	if !foundBoundaryPinned {
		t.Error("compaction boundary should restore pinned=true")
	}
}

func TestBuildCompactedChain_PinnedExceedsBudgetNoTail(t *testing.T) {
	// Large pinned messages consume the entire tail budget.
	longContent := strings.Repeat("x", 10_000)
	messages := []api.Message{
		{Role: api.RoleUser, Content: longContent, Pinned: true},
		{Role: api.RoleUser, Content: longContent, Pinned: true},
		{Role: api.RoleUser, Content: "should-not-appear-in-tail"},
		{Role: api.RoleUser, Content: "also-excluded"},
	}

	target := estimateTokens(messages[:2]) + estimateMessageTokens(api.Message{
		Role:    api.RoleUser,
		Content: "<system-reminder>\n" + compactSummaryPrefix + "summary\n</system-reminder>",
	})

	result := buildCompactedChain(messages, "summary", target)

	if !isCompactionBoundary(result[0]) {
		t.Fatal("first message should be boundary")
	}
	for _, msg := range result {
		if msg.Content == "should-not-appear-in-tail" || msg.Content == "also-excluded" {
			t.Errorf("non-pinned tail should be dropped when pinned exceeds budget, got %q", msg.Content)
		}
	}
	if len(result) < 3 {
		t.Errorf("expected boundary + pinned messages, got %d messages", len(result))
	}
}

func TestRebuildMessages_BoundaryPinnedWithoutFlag(t *testing.T) {
	entries := []session.TranscriptEntry{
		{
			Type:    session.EntryTypeSystem,
			Subtype: session.SubtypeCompactBoundary,
			CompactMetadata: &session.CompactMetadata{
				Summary: "legacy summary without pinned field",
			},
		},
	}
	msgs := RebuildMessages(entries)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].Pinned {
		t.Error("compaction boundary should be pinned even when transcript entry omits pinned field")
	}
}
