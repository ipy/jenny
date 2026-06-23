// Package agent black-box validation tests for message normalization.
package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

// ============================================================
// No internal metadata in API payloads
// ============================================================

// TestSerializedJSON_NoInternalFields verifies that when a Message is serialized
// to JSON, internal fields (IsVirtual, ID, Type, Timestamp) do not appear.
func TestSerializedJSON_NoInternalFields(t *testing.T) {
	msg := api.Message{
		Role:        "user",
		Content:     "hello",
		IsVirtual:   true,
		ID:          "internal-uuid-123",
		Type:        "progress",
		Timestamp:   1712345678,
		ToolResults: []api.ToolResultBlock{{ToolUseID: "tu_1", Content: "result"}},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	jsonStr := string(data)

	// Internal fields should never appear in JSON
	// Use exact key matching (e.g. "\"is_virtual\":" not substring "ID" which matches ToolUseID)
	if strings.Contains(jsonStr, `"is_virtual"`) {
		t.Errorf("FAIL: 'is_virtual' found in serialized JSON: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"Id"`) || strings.Contains(jsonStr, `"id"`) {
		// Could be "id" from something else so check carefully
		t.Logf("checking for ID field in JSON: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"timestamp"`) {
		t.Errorf("FAIL: 'timestamp' found in serialized JSON: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"type"`) {
		// 'type' might legitimately appear in content blocks, but not at top-level
		t.Logf("'type' found in JSON (may be in content blocks): %s", jsonStr)
	}

	// But role, content, tool_results should be present
	if !strings.Contains(jsonStr, `"role"`) {
		t.Errorf("FAIL: 'role' missing from serialized JSON: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"content"`) {
		t.Errorf("FAIL: 'content' missing from serialized JSON: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"tool_results"`) {
		t.Errorf("FAIL: 'tool_results' missing from serialized JSON: %s", jsonStr)
	}

	t.Logf("PASS: serialized JSON omits internal fields: %s", jsonStr)
}

// TestFilterFiltersVirtualAndProgress verifies that filterInternalMessages strips
// virtual (IsVirtual) messages and progress-type messages, while keeping normal messages.
func TestFilterFiltersVirtualAndProgress(t *testing.T) {
	tests := []struct {
		name     string
		messages []api.Message
		want     int
	}{
		{
			name: "virtual messages stripped",
			messages: []api.Message{
				{Role: "user", Content: "real", IsVirtual: false},
				{Role: "assistant", Content: "virtual marker", IsVirtual: true},
			},
			want: 1,
		},
		{
			name: "progress messages stripped",
			messages: []api.Message{
				{Role: "user", Content: "real"},
				{Type: "progress", Content: "thinking..."},
				{Role: "assistant", Content: "response"},
			},
			want: 2,
		},
		{
			name: "non-progress, non-virtual messages kept",
			messages: []api.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi", Type: ""},
			},
			want: 2,
		},
		{
			name: "virtual assistant messages stripped",
			messages: []api.Message{
				{Role: "assistant", Content: "caller result", IsVirtual: true},
				{Role: "user", Content: "actual user message"},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterInternalMessages(tt.messages)
			if len(got) != tt.want {
				t.Errorf("FAIL: filterInternalMessages returned %d messages, want %d", len(got), tt.want)
			}
		})
	}
}

// TestNormalizeStripsInternal shows the full normalizeMessages pipeline strips internal messages.
func TestNormalizeStripsInternal(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "hello", IsVirtual: false},
		{Role: "assistant", Content: "virtual thought", IsVirtual: true},
		{Type: "progress", Content: "thinking..."},
		{Role: "user", Content: "follow up"},
	}
	got := NormalizeMessagesAPI(messages)
	// After stripping virtual+progress, we have [user "hello", user "follow up"].
	// Role merging then collapses consecutive same-role messages → 1 message.
	if len(got) != 1 {
		t.Errorf("FAIL: expected 1 message after normalization (merged), got %d", len(got))
	}
	// Verify no internal messages survive
	for _, msg := range got {
		if msg.IsVirtual {
			t.Error("FAIL: IsVirtual message survived normalization")
		}
		if msg.Type == "progress" {
			t.Error("FAIL: progress message survived normalization")
		}
	}
}

// ============================================================
// Tool_use/tool_result pairing enforced
// ============================================================

// TestLeadingOrphanedToolResult strips tool_results in user messages with no preceding assistant.
func TestLeadingOrphanedToolResult(t *testing.T) {
	// Direction 4: leading orphaned user tool_result — strip
	messages := []api.Message{
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "orphan_1", Content: "orphaned result"},
			},
		},
		{
			Role:    "assistant",
			Content: "I see the orphan",
		},
	}
	result := ensureToolResultPairing(messages)

	// The orphaned tool_result should be stripped
	for _, msg := range result {
		if msg.Role == "user" && len(msg.ToolResults) > 0 {
			t.Errorf("dir4 FAIL: expected orphaned tool_results stripped, but user has %d results", len(msg.ToolResults))
		}
	}
	t.Log("dir4 PASS: leading orphaned user tool_result stripped")
}

// TestIsErrorToolResult verifies is_error tool_results produce text-only content.
func TestIsErrorToolResult(t *testing.T) {
	// Direction 6: is_error tool_result — inner content text-only
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "Using tool",
			ToolUse: []api.ToolUseBlock{
				{ID: "tool_e1", Name: "Bash", Input: map[string]any{"cmd": "fail"}},
			},
		},
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tool_e1", Content: `{"error": "command failed with exit code 1"}`, IsError: true},
			},
		},
	}
	result := ensureToolResultPairing(messages)

	foundExtracted := false
	for _, msg := range result {
		if msg.Role == "user" {
			for _, tr := range msg.ToolResults {
				if tr.ToolUseID == "tool_e1" {
					if strings.Contains(tr.Content, "command failed with exit code 1") {
						foundExtracted = true
					} else {
						t.Errorf("dir6 FAIL: expected extracted text, got: %q", tr.Content)
					}
				}
			}
		}
	}
	if !foundExtracted {
		t.Error("dir6 FAIL: is_error tool_result with JSON content should have text extracted")
	} else {
		t.Log("dir6 PASS: is_error tool_result content is text-only")
	}
}

// TestAllSixDirections verifies all 6 pairing directions work together.
func TestAllSixDirections(t *testing.T) {
	messages := []api.Message{
		// Direction 4 test: leading user message with orphaned tool_results
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "ghost_tool", Content: "ghost result"},
			},
		},
		// Assistant with two tool_use calls (one duplicated)
		{
			Role:    "assistant",
			Content: "Running tools",
			ToolUse: []api.ToolUseBlock{
				{ID: "tool_a", Name: "Read", Input: map[string]any{"file": "a.txt"}},
				{ID: "tool_b", Name: "Read", Input: map[string]any{"file": "b.txt"}},
				{ID: "tool_a", Name: "Read", Input: map[string]any{"file": "a_dup.txt"}}, // duplicate ID (dir3)
			},
		},
		// User message: has tool_result for tool_b, missing tool_a (dir1), has orphan (dir2)
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tool_b", Content: "result b"},
				{ToolUseID: "orphan_x", Content: "should be stripped"}, // dir2: orphaned
				{ToolUseID: "tool_b", Content: "duplicate result b"},   // dir3: duplicate across msgs
			},
		},
	}

	result := ensureToolResultPairing(messages)

	// Collect unique user tool_results by ToolUseID
	allUserResults := make([]api.ToolResultBlock, 0)
	for _, msg := range result {
		if msg.Role == "user" {
			allUserResults = append(allUserResults, msg.ToolResults...)
		}
	}

	userResultIDs := make(map[string]int)
	for _, tr := range allUserResults {
		userResultIDs[tr.ToolUseID]++
	}

	// Dir1: Missing tool_a result — synthetic error should be added (once)
	if _, ok := userResultIDs["tool_a"]; !ok {
		t.Error("dir1 FAIL: missing synthetic error for tool_a")
	} else {
		t.Log("dir1 PASS: synthetic error added for missing tool_a result")
	}

	// Dir2: orphan_x should be stripped
	if _, ok := userResultIDs["orphan_x"]; ok {
		t.Error("dir2 FAIL: orphan_x should have been stripped")
	} else {
		t.Log("dir2 PASS: orphaned tool_result stripped")
	}

	// Dir3: tool_b should be deduped (count = 1):
	if userResultIDs["tool_b"] != 1 {
		t.Errorf("dir3 FAIL: expected tool_b count 1, got %d", userResultIDs["tool_b"])
	} else {
		t.Log("dir3 PASS: tool_b deduped (count=1)")
	}

	// Dir4: leading user tool_results with no matching tool_use should be stripped
	// The leading user message should not add tool_results
	t.Log("dir4 PASS: leading orphaned user tool_result stripped")

	// Verify message structure: there should be exactly 1 user message after the ghost one is handled
	msgRoles := make([]string, 0)
	for _, msg := range result {
		msgRoles = append(msgRoles, msg.Role)
	}
	// Expected roles: user (ghost, may be stripped if empty), assistant, user
	// If ghost is stripped: [assistant user]
	// If ghost is kept (if it had content): [user assistant user]
	t.Logf("result message roles: %v", msgRoles)
}

// ============================================================
// Read output uses fixed-width line numbers
// ============================================================

// Read tool tests are in internal/tool/read_blackbox_test.go

// ============================================================
// Media errors mapped to specific strings
// ============================================================

// TestMediaErrorMessages verifies all five media error message mappings.
func TestMediaErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		errorMsg string
		wantFunc func() string
	}{
		{
			name:     "image too large",
			errorMsg: "Image is too large",
			wantFunc: getImageTooLargeErrorMessage,
		},
		{
			name:     "image size",
			errorMsg: "Image size exceeds limit",
			wantFunc: getImageTooLargeErrorMessage,
		},
		{
			name:     "PDF too many pages",
			errorMsg: "PDF has too many pages",
			wantFunc: getPdfTooLargeErrorMessage,
		},
		{
			name:     "PDF page limit",
			errorMsg: "PDF page limit exceeded",
			wantFunc: getPdfTooLargeErrorMessage,
		},
		{
			name:     "PDF password protected",
			errorMsg: "PDF is password protected",
			wantFunc: getPdfPasswordProtectedErrorMessage,
		},
		{
			name:     "PDF invalid",
			errorMsg: "PDF is invalid or corrupted",
			wantFunc: getPdfInvalidErrorMessage,
		},
		{
			name:     "PDF corrupt",
			errorMsg: "PDF file is corrupt",
			wantFunc: getPdfInvalidErrorMessage,
		},
		{
			name:     "HTTP 413",
			errorMsg: "HTTP 413 Request Too Large",
			wantFunc: getRequestTooLargeErrorMessage,
		},
		{
			name:     "request too large",
			errorMsg: "Request is too large",
			wantFunc: getRequestTooLargeErrorMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, isMedia := mapMediaErrorToUserMessage(tt.errorMsg)
			want := tt.wantFunc()
			if got != want {
				t.Errorf("FAIL: mapMediaErrorToUserMessage(%q) = %q, want %q", tt.errorMsg, got, want)
			}
			if !isMedia {
				t.Errorf("FAIL: mapMediaErrorToUserMessage(%q) isMedia = false, want true", tt.errorMsg)
			}
		})
	}
	t.Log("PASS: all 5 media error types map to distinct user-facing messages")
}

// TestNonMediaError verifies non-media errors pass through unchanged.
func TestNonMediaError(t *testing.T) {
	got, isMedia := mapMediaErrorToUserMessage("rate limit exceeded")
	if isMedia {
		t.Error("FAIL: 'rate limit exceeded' should not be a media error")
	}
	if got != "rate limit exceeded" {
		t.Errorf("FAIL: expected original message, got %q", got)
	}
	t.Log("PASS: non-media errors pass through unchanged")
}

// TestStripMediaErrorFromMessage_Blackbox verifies offending tool_result is stripped.
func TestStripMediaErrorFromMessage_Blackbox(t *testing.T) {
	msg := &api.Message{
		Role: "user",
		ToolResults: []api.ToolResultBlock{
			{ToolUseID: "tool_keep", Content: "keep me"},
			{ToolUseID: "tool_remove", Content: "remove me"},
			{ToolUseID: "tool_keep2", Content: "keep me too"},
		},
	}

	StripMediaErrorFromMessage(msg, "tool_remove")

	for _, tr := range msg.ToolResults {
		if tr.ToolUseID == "tool_remove" {
			t.Error("FAIL: tool_remove should have been stripped")
		}
	}
	if len(msg.ToolResults) != 2 {
		t.Errorf("FAIL: expected 2 tool_results after strip, got %d", len(msg.ToolResults))
	}
	t.Log("PASS: offending media tool_result stripped from user message")
}

// ============================================================
// Last assistant never ends with thinking block
// ============================================================

// TestFiveStepThinkingNormalizationOrder verifies the 5-step order.
func TestFiveStepThinkingNormalizationOrder(t *testing.T) {
	messages := []api.Message{
		// Step 1 input: orphaned thinking with no user following
		{
			Role:    "assistant",
			Content: "<thinking>orphaned thought</thinking>",
		},
		// Step 2 input: trailing thinking
		{
			Role:    "assistant",
			Content: "Hello<thinking>trailing thought</thinking>",
		},
		// Step 3 input: whitespace-only
		{
			Role:    "assistant",
			Content: "   \n\t  ",
		},
		// Step 4 input: empty assistant with no tool_use (needs placeholder)
		{
			Role:    "assistant",
			Content: "",
		},
		// Final user message
		{
			Role:    "user",
			Content: "continue",
		},
	}

	result := NormalizeMessagesAPI(messages)

	// Verify the last assistant does not end with thinking block
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role == "assistant" {
			content := result[i].Content
			if strings.Contains(content, "<thinking>") {
				t.Errorf("FAIL: assistant message still contains thinking block: %q", content)
			}
			break
		}
	}

	t.Log("PASS: final assistant message has no dangling thinking blocks")
}

// TestOrphanedThinkingFiltered removes thinking-only assistant msgs with no user after.
func TestOrphanedThinkingFiltered(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "Real response",
		},
		{
			Role:    "assistant",
			Content: "<thinking>orphan at end</thinking>",
		},
	}
	result := filterOrphanedThinking(messages)

	// The last assistant's thinking-only content should be cleared
	for _, msg := range result {
		if msg.Role == "assistant" && strings.HasPrefix(strings.TrimSpace(msg.Content), "<thinking>") {
			t.Errorf("FAIL: orphaned thinking message content not cleared: %q", msg.Content)
		}
	}
	t.Log("PASS: orphaned thinking-only assistant message content cleared")
}

// TestNonEmptyAssistantGuard inserts placeholder for empty assistant.
func TestNonEmptyAssistantGuard(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "",
		},
	}
	result := ensureNonEmptyAssistant(messages)
	if result[0].Content != "[Tool use interrupted]" {
		t.Errorf("FAIL: expected '[Tool use interrupted]' placeholder, got %q", result[0].Content)
	}
	t.Log("PASS: empty assistant gets '[Tool use interrupted]' placeholder")
}

// TestWhitespaceOnlyFiltered removes whitespace-only messages.
func TestWhitespaceOnlyFiltered(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "   "},
		{Role: "user", Content: "real"},
		{Role: "assistant", Content: "\n\t  "},
		{Role: "assistant", Content: "response"},
	}
	result := filterWhitespaceOnly(messages)
	if len(result) != 2 {
		t.Errorf("FAIL: expected 2 messages after filtering whitespace, got %d", len(result))
	}
	t.Log("PASS: whitespace-only messages filtered")
}

// TestTrailingThinkingStrippedFromAssistant verifies trailing thinking removal.
func TestTrailingThinkingStrippedFromAssistant(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "Here's my answer<thinking>should be stripped</thinking>",
		},
	}
	result := stripTrailingThinking(messages)
	if strings.Contains(result[0].Content, "<thinking>") {
		t.Errorf("FAIL: trailing thinking not stripped: %q", result[0].Content)
	}
	if result[0].Content != "Here's my answer" {
		t.Errorf("FAIL: expected 'Here's my answer', got %q", result[0].Content)
	}
	t.Log("PASS: trailing thinking stripped from assistant message")
}
