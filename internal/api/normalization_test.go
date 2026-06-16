package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// TestNormalization_ToolResultFlattening_EdgeCases verifies the tool_result content
// flattening pass produces correct wire format for all edge cases (AC1-AC5).
// Run: go test ./internal/api/ -run "TestNormalization" -v -count=1
// Expected: 5 PASS results (all 5 AC subtests pass).
//
// Prior fixes:
// - 95f5153: flatten tool_result content for DeepSeek compatibility
// - 4e84e9c: add comprehensive edge-case tests
// - 514fb98: add t.Cleanup to clear request inspector after AC4
// - decdb7c: use LIFO indexing (reqs[len(reqs)-1]) instead of FIFO (reqs[0])
func TestNormalization_ToolResultFlattening_EdgeCases(t *testing.T) {
	mock := mockapi.NewMockServer()
	defer mock.Close()

	// Helper to create provider
	setupProvider := func(t *testing.T, cassetteID string) (*anthropicProvider, string) {
		baseURL := mock.URL() + "/cassette/" + cassetteID
		t.Setenv("ANTHROPIC_BASE_URL", baseURL)
		t.Setenv("ANTHROPIC_API_KEY", "test-key")
		provider, err := newAnthropicProvider("claude-3-sonnet-20240229")
		if err != nil {
			t.Fatalf("Failed to create provider: %v", err)
		}
		provider.SetMaxTokensOverride(1000)
		return provider, baseURL
	}

	mock.SetContentType("test", "application/json")
	mock.SetInlineResponse("test", `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-3-sonnet-20240229","stop_reason":"end_turn"}`)

	t.Run("AC1: Empty tool_result content serializes as empty string", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{
						ToolUseID: "call_1",
						Content:   "",
					},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		if len(reqs) == 0 {
			t.Fatal("No requests captured")
		}

		// Verify content is a string ""
		found := false
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		for _, m := range msgs {
			msg, _ := m.(map[string]any)
			content, _ := msg["content"].([]any)
			for _, b := range content {
				block, _ := b.(map[string]any)
				if block["type"] == "tool_result" {
					found = true
					trContent, isString := block["content"].(string)
					if !isString {
						t.Errorf("tool_result content is not a string: %v", block["content"])
					}
					if trContent != "" {
						t.Errorf("expected empty string content, got %q", trContent)
					}
				}
			}
		}
		if !found {
			t.Error("tool_result block not found in request")
		}
	})

	t.Run("AC2: Multiple tool_results in one user message", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "Output A"},
					{ToolUseID: "call_2", Content: "Output B"},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := lastMsg["content"].([]any)

		count := 0
		for _, b := range content {
			block, _ := b.(map[string]any)
			if block["type"] == "tool_result" {
				count++
				if _, isString := block["content"].(string); !isString {
					t.Errorf("block %d: tool_result content is not a string", count)
				}
			}
		}
		if count != 2 {
			t.Errorf("expected 2 tool_result blocks, got %d", count)
		}
	})

	t.Run("AC3: Error tool_result preserves is_error: true", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{
						ToolUseID: "call_1",
						Content:   "error details",
						IsError:   true,
					},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := lastMsg["content"].([]any)
		block, _ := content[0].(map[string]any)

		if block["is_error"] != true {
			t.Error("expected is_error: true in tool_result block")
		}
		if block["content"] != "error details" {
			t.Errorf("expected content 'error details', got %v", block["content"])
		}
	})

	t.Run("AC4: Mock server rejects array content, test passes", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		// Inspector that rejects array-formatted tool_results
		mock.SetRequestInspector(func(r mockapi.APIRequest) error {
			msgs, _ := r.Body["messages"].([]any)
			for _, m := range msgs {
				msg, _ := m.(map[string]any)
				content, _ := msg["content"].([]any)
				for _, b := range content {
					block, _ := b.(map[string]any)
					if block["type"] == "tool_result" {
						if _, isArray := block["content"].([]any); isArray {
							return fmt.Errorf("REJECTED: tool_result content is an array")
						}
					}
				}
			}
			return nil
		})
		t.Cleanup(func() { mock.SetRequestInspector(nil) })

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "some output"},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v (flattening might be broken)", err)
		}
		// If SendMessage succeeds, the inspector didn't return an error, meaning content was NOT an array.
	})

	t.Run("AC5: Non-tool_result blocks untouched", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "assistant",
				ToolUse: []ToolUseBlock{
					{
						ID:    "call_1",
						Name:  "test_tool",
						Input: map[string]any{"key": "value"},
					},
				},
			},
			{
				Role:    "user",
				Content: "Hello",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "prev output"},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)

		// Check assistant message for tool_use
		assistantMsg, _ := msgs[0].(map[string]any)
		assistantContent, _ := assistantMsg["content"].([]any)
		hasToolUse := false
		for _, b := range assistantContent {
			block, _ := b.(map[string]any)
			if block["type"] == "tool_use" {
				hasToolUse = true
			}
		}

		// Check user message for text and tool_result
		userMsg, _ := msgs[1].(map[string]any)
		userContent, _ := userMsg["content"].([]any)

		hasText := false
		hasToolResult := false

		for _, b := range userContent {
			block, _ := b.(map[string]any)
			switch block["type"] {
			case "text":
				hasText = true
				if _, isString := block["text"].(string); !isString {
					t.Error("text block 'text' field is not a string")
				}
			case "tool_result":
				hasToolResult = true
				if _, isString := block["content"].(string); !isString {
					t.Error("tool_result block 'content' field is not a string")
				}
			}
		}

		if !hasText || !hasToolUse || !hasToolResult {
			t.Errorf("missing blocks: text=%v, tool_use=%v, tool_result=%v", hasText, hasToolUse, hasToolResult)
		}
	})
}

// TestNormalization_CredentialBoundArtifactStripping verifies that redacted_thinking
// blocks are stripped from message history when a session is resumed with a different
// API key (SSNF Pass 2.D).
// Run: go test ./internal/api/ -run "TestNormalization" -v -count=1
// Expected: 5 PASS results (all 5 AC subtests pass).
func TestNormalization_CredentialBoundArtifactStripping(t *testing.T) {
	findSubstring := func(s, substr string) bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}

	containsString := func(s, substr string) bool {
		return len(s) >= len(substr) && findSubstring(s, substr)
	}

	containsRedactedThinking := func(content string) bool {
		return len(content) > 0 && (containsString(content, `<thinking type="redacted">`) || containsString(content, `"type":"redacted_thinking"`))
	}

	t.Run("AC1: Single redacted_thinking block stripped", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "current-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		// OriginalAPIKey is different, so stripping should occur
		caps := Capabilities{OriginalAPIKey: "original-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_DATA_12345</thinking>`,
			},
		}

		normalized, _, logs := NormalizeMessages(messages, nil, caps)

		// Verify the redacted_thinking block was stripped
		if normalized[0].Content != "" {
			t.Errorf("redacted_thinking block should have been stripped, but content is: %s", normalized[0].Content)
		}

		// Verify NormalizationLog entry exists
		foundLog := false
		for _, log := range logs {
			if log.Pass == "StripCredentialBoundArtifacts" {
				foundLog = true
				break
			}
		}
		if !foundLog {
			t.Error("expected NormalizationLog entry for StripCredentialBoundArtifacts")
		}
	})

	t.Run("AC2: Multiple messages with redacted_thinking blocks stripped", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "current-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		caps := Capabilities{OriginalAPIKey: "original-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_1</thinking>`,
				ToolUse: []ToolUseBlock{
					{ID: "call_1", Name: "tool1", Input: map[string]any{}},
				},
			},
			{
				Role:    "user",
				Content: "test",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "result"},
				},
			},
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_2</thinking><thinking type="redacted">SIG_3</thinking>`,
			},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, caps)

		// First message: tool_use preserved, content stripped
		if normalized[0].Content != "" {
			t.Errorf("first message content should be empty after stripping, got: %s", normalized[0].Content)
		}
		if len(normalized[0].ToolUse) != 1 {
			t.Errorf("first message tool_use should be preserved, got %d", len(normalized[0].ToolUse))
		}

		// Second message (user): content preserved as-is
		if normalized[1].Content != "test" {
			t.Errorf("second message content should be preserved, got: %s", normalized[1].Content)
		}

		// Third message: all redacted blocks stripped, content empty
		if normalized[2].Content != "" {
			t.Errorf("third message content should be empty after stripping, got: %s", normalized[2].Content)
		}
	})

	t.Run("AC3: Non-redacted thinking preserved", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "current-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		caps := Capabilities{OriginalAPIKey: "original-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking>valid chain of thought</thinking><thinking type="redacted">SIG</thinking>`,
			},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, caps)

		// Non-redacted thinking should be preserved
		if !containsString(normalized[0].Content, `<thinking>valid chain of thought</thinking>`) {
			t.Errorf("non-redacted thinking should be preserved, but content is: %s", normalized[0].Content)
		}

		// Redacted thinking should be stripped
		if containsRedactedThinking(normalized[0].Content) {
			t.Errorf("redacted_thinking should be stripped, but found in: %s", normalized[0].Content)
		}
	})

	t.Run("AC4: Stripping inactive when key matches", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "test-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "test-key")

		// When OriginalAPIKey matches current ANTHROPIC_API_KEY, stripping should be skipped
		caps := Capabilities{OriginalAPIKey: "test-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_DATA</thinking>`,
			},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, caps)

		// When keys match, redacted_thinking should be preserved
		if !containsRedactedThinking(normalized[0].Content) {
			t.Errorf("redacted_thinking should be preserved when keys match, but content is: %s", normalized[0].Content)
		}
	})

	t.Run("AC5: NormalizationLog entry on strip", func(t *testing.T) {
		// Test that NormalizationLog is properly populated
		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_DATA</thinking><thinking type="redacted">SIG_DATA_2</thinking>`,
			},
		}

		// Save and restore original env
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		caps := Capabilities{OriginalAPIKey: "original-key"}
		_, _, logs := NormalizeMessages(messages, nil, caps)

		// Verify log entry
		foundLog := false
		for _, log := range logs {
			if log.Pass == "StripCredentialBoundArtifacts" {
				foundLog = true
				// Should mention 2 blocks stripped
				if !containsString(log.Message, "2") {
					t.Errorf("log message should mention 2 blocks stripped: %s", log.Message)
				}
				break
			}
		}
		if !foundLog {
			t.Error("expected NormalizationLog entry for StripCredentialBoundArtifacts")
		}
	})
}

// TestNormalizeTools_EmptySchema verifies that NormalizeTools injects __arg__ placeholder
// for tools with empty properties.
func TestNormalizeTools_EmptySchema(t *testing.T) {
	tools := []ToolParam{
		{
			Name:        "Bash",
			Description: "Run a bash command",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
	}

	result := NormalizeTools(tools, Capabilities{}, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	props := result[0].InputSchema.Properties
	if props == nil {
		t.Fatal("Properties should not be nil")
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 property, got %d", len(props))
	}
	if _, ok := props["__arg__"]; !ok {
		t.Error("expected __arg__ placeholder to be injected")
	}
}

// TestNormalizeTools_NilSchema verifies that NormalizeTools handles nil Properties
// by initializing and injecting __arg__.
func TestNormalizeTools_NilSchema(t *testing.T) {
	tools := []ToolParam{
		{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: ToolInputSchema{
				Type: "object",
			},
		},
	}

	result := NormalizeTools(tools, Capabilities{}, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	props := result[0].InputSchema.Properties
	if props == nil {
		t.Fatal("Properties should be initialized")
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 property, got %d", len(props))
	}
	if _, ok := props["__arg__"]; !ok {
		t.Error("expected __arg__ placeholder to be injected for nil properties")
	}
}

// TestNormalizeTools_PreservesNonEmptySchema verifies that NormalizeTools does not
// modify tools that already have properties defined.
func TestNormalizeTools_PreservesNonEmptySchema(t *testing.T) {
	tools := []ToolParam{
		{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"path": map[string]any{"type": "string"},
				},
				Required: []string{"path"},
			},
		},
	}

	result := NormalizeTools(tools, Capabilities{}, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	props := result[0].InputSchema.Properties
	if _, ok := props["__arg__"]; ok {
		t.Error("should not inject __arg__ for tool with existing properties")
	}
	if _, ok := props["path"]; !ok {
		t.Error("existing property 'path' should be preserved")
	}
}

// TestNormalizeTools_StripsBetaFields verifies that NormalizeTools strips experimental
// beta fields (defer_loading, cache_control, eager_input_streaming) when
// DisableExperimentalBetas is true.
func TestNormalizeTools_StripsBetaFields(t *testing.T) {
	tools := []ToolParam{
		{
			Name:        "Bash",
			Description: "Run a bash command",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"cmd": map[string]any{"type": "string"},
				},
				ExtraFields: map[string]any{
					"defer_loading":         true,
					"cache_control":         map[string]any{"type": "ephemeral"},
					"eager_input_streaming": true,
					"$defs":                 map[string]any{},
				},
			},
		},
	}

	caps := Capabilities{DisableExperimentalBetas: true}
	result := NormalizeTools(tools, caps, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	extra := result[0].InputSchema.ExtraFields
	if extra == nil {
		t.Fatal("ExtraFields should not be nil")
	}
	if _, ok := extra["defer_loading"]; ok {
		t.Error("defer_loading should be stripped when DisableExperimentalBetas is true")
	}
	if _, ok := extra["cache_control"]; ok {
		t.Error("cache_control should be stripped when DisableExperimentalBetas is true")
	}
	if _, ok := extra["eager_input_streaming"]; ok {
		t.Error("eager_input_streaming should be stripped when DisableExperimentalBetas is true")
	}
	// $defs should be preserved (not a beta field)
	if _, ok := extra["$defs"]; !ok {
		t.Error("$defs should be preserved (not a beta field)")
	}
}

// TestNormalizeTools_EmptyTools verifies that NormalizeTools handles empty tools slice.
func TestNormalizeTools_EmptyTools(t *testing.T) {
	tools := []ToolParam{}
	result := NormalizeTools(tools, Capabilities{}, nil)
	if result == nil {
		t.Fatal("result should not be nil for empty input")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 tools, got %d", len(result))
	}
}

// TestNormalizeMessages_CallsNormalizeTools verifies that NormalizeMessages applies
// tool schema stabilization by checking that __arg__ is injected and beta fields
// are stripped as part of the normalization pipeline.
func TestNormalizeMessages_CallsNormalizeTools(t *testing.T) {
	t.Run("injects __arg__ for empty schema", func(t *testing.T) {
		tools := []ToolParam{
			{
				Name:        "Bash",
				Description: "Run a bash command",
				InputSchema: ToolInputSchema{
					Type:       "object",
					Properties: map[string]any{},
				},
			},
		}

		_, normalizedTools, logs := NormalizeMessages(nil, tools, Capabilities{})

		if len(normalizedTools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(normalizedTools))
		}
		props := normalizedTools[0].InputSchema.Properties
		if _, ok := props["__arg__"]; !ok {
			t.Error("expected __arg__ placeholder to be injected via NormalizeMessages")
		}
		// Check log entry exists
		foundLog := false
		for _, log := range logs {
			if log.Pass == "EmptySchemaPlaceholder" {
				foundLog = true
				break
			}
		}
		if !foundLog {
			t.Error("expected NormalizationLog entry for EmptySchemaPlaceholder")
		}
	})

	t.Run("strips beta fields when DisableExperimentalBetas is true", func(t *testing.T) {
		tools := []ToolParam{
			{
				Name:        "Bash",
				Description: "Run a bash command",
				InputSchema: ToolInputSchema{
					Type: "object",
					Properties: map[string]any{
						"cmd": map[string]any{"type": "string"},
					},
					ExtraFields: map[string]any{
						"defer_loading": true,
					},
				},
			},
		}

		caps := Capabilities{DisableExperimentalBetas: true}
		_, normalizedTools, _ := NormalizeMessages(nil, tools, caps)

		if len(normalizedTools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(normalizedTools))
		}
		extra := normalizedTools[0].InputSchema.ExtraFields
		if _, ok := extra["defer_loading"]; ok {
			t.Error("defer_loading should be stripped when DisableExperimentalBetas is true")
		}
	})
}

// TestSSNF_ContentBlockValidation tests SSNF Pass 2C: Content Block Validation.
// These tests verify the edge cases that occur during session resume.
// Run: go test ./internal/api/ -run "TestSSNF_ContentBlockValidation" -v -count=1
func TestSSNF_ContentBlockValidation(t *testing.T) {
	t.Run("AC1: whitespace-only text block stripped", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "   \n\t  "}, // whitespace-only content
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Whitespace-only content should be stripped to empty
		if normalized[1].Content != "" {
			t.Errorf("expected whitespace-only content to be stripped, got %q", normalized[1].Content)
		}
	})

	t.Run("AC1: non-whitespace text preserved", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "  Hello world  "}, // has actual content
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		if normalized[1].Content != "  Hello world  " {
			t.Errorf("expected content to be preserved, got %q", normalized[1].Content)
		}
	})

	t.Run("AC2: empty content in non-final assistant gets placeholder", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "", ToolUse: nil}, // empty content, no tool_use
			{Role: RoleUser, Content: "Thanks"}, // another user message makes this non-final
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Second assistant message should get [No content] placeholder
		if normalized[1].Content != "[No content]" {
			t.Errorf("expected [No content] placeholder, got %q", normalized[1].Content)
		}
	})

	t.Run("AC2: final assistant message empty allowed", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "", ToolUse: nil}, // empty, no tool_use
			// This is the final message - used for prefill, so empty is allowed
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Final assistant message should remain empty (for prefill)
		if normalized[1].Content != "" {
			t.Errorf("expected final assistant empty content to be preserved, got %q", normalized[1].Content)
		}
	})

	t.Run("AC2: assistant with tool_use empty content allowed", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "", ToolUse: []ToolUseBlock{
				{ID: "call_1", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
			}},
			{Role: RoleUser, Content: "Done"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Assistant with tool_use should keep empty content (tool call expected)
		if normalized[1].Content != "" {
			t.Errorf("expected assistant with tool_use to keep empty content, got %q", normalized[1].Content)
		}
	})

	t.Run("AC3: trailing thinking block stripped", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Let me think about this.<thinking>analyzing the problem</thinking>"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Trailing thinking block should be stripped
		if normalized[1].Content != "Let me think about this." {
			t.Errorf("expected trailing thinking block stripped, got %q", normalized[1].Content)
		}
	})

	t.Run("AC3: trailing redacted_thinking block stripped", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Here's my response.<thinking type=\"redacted\">SIG_DATA</thinking>"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Trailing redacted_thinking block should be stripped
		if normalized[1].Content != "Here's my response." {
			t.Errorf("expected trailing redacted_thinking block stripped, got %q", normalized[1].Content)
		}
	})

	t.Run("AC3: multiple trailing thinking blocks stripped", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Done.<thinking>thought 1</thinking><thinking>thought 2</thinking>"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// All trailing thinking blocks should be stripped
		if normalized[1].Content != "Done." {
			t.Errorf("expected all trailing thinking blocks stripped, got %q", normalized[1].Content)
		}
	})

	t.Run("AC3: thinking block in middle preserved", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "<thinking>thinking</thinking>Here is my response."},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Thinking block in middle should be preserved (not trailing)
		expectedContent := "<thinking>thinking</thinking>Here is my response."
		if normalized[1].Content != expectedContent {
			t.Errorf("expected thinking block in middle to be preserved, got %q", normalized[1].Content)
		}
	})

	t.Run("AC4: orphaned thinking-only message dropped", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "<thinking>only thinking</thinking>"},
			{Role: RoleUser, Content: "Thanks"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Thinking-only message should be dropped
		if len(normalized) != 2 {
			t.Errorf("expected 2 messages after dropping thinking-only, got %d", len(normalized))
		}
		// Verify the user message is preserved
		if normalized[1].Role != RoleUser || normalized[1].Content != "Thanks" {
			t.Errorf("expected user message preserved, got %+v", normalized[1])
		}
	})

	t.Run("AC4: orphaned redacted_thinking-only message dropped", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "<thinking type=\"redacted\">SIG</thinking>"},
			{Role: RoleUser, Content: "Thanks"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Redacted thinking-only message should be dropped
		if len(normalized) != 2 {
			t.Errorf("expected 2 messages after dropping redacted thinking-only, got %d", len(normalized))
		}
	})

	t.Run("AC4: assistant with text and thinking preserved", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "<thinking>thinking</thinking>Here is my response."},
			{Role: RoleUser, Content: "Thanks"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Assistant message with text should be preserved
		if len(normalized) != 3 {
			t.Errorf("expected 3 messages, got %d", len(normalized))
		}
		if normalized[1].Role != RoleAssistant {
			t.Errorf("expected assistant message at index 1")
		}
	})

	t.Run("AC4: assistant with tool_use preserved even if content is thinking", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "<thinking>thinking</thinking>", ToolUse: []ToolUseBlock{
				{ID: "call_1", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
			}},
			{Role: RoleUser, Content: "Thanks"},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Assistant with tool_use should be preserved
		if len(normalized) != 3 {
			t.Errorf("expected 3 messages (tool_use preserved), got %d", len(normalized))
		}
	})

	t.Run("AC4: orphaned thinking-only message dropped when final in history", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "<thinking>only thinking</thinking>"},
			// This thinking-only message is the final message — must still be dropped
		}

		normalized, _, _ := NormalizeMessages(messages, nil, Capabilities{})

		// Thinking-only message must be dropped even though it is the final message
		if len(normalized) != 1 {
			t.Errorf("expected 1 message after dropping final-position thinking-only, got %d", len(normalized))
		}
		if normalized[0].Role != RoleUser {
			t.Errorf("expected user message preserved, got role=%s", normalized[0].Role)
		}
	})

	t.Run("AC5: normalization logs include content block validation", func(t *testing.T) {
		messages := []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "   \n\t  "}, // whitespace-only
			{Role: RoleUser, Content: "Thanks"},
		}

		_, _, logs := NormalizeMessages(messages, nil, Capabilities{})

		// Check that ContentBlockValidation logs are generated
		foundValidationLog := false
		for _, log := range logs {
			if log.Pass == "ContentBlockValidation" {
				foundValidationLog = true
				break
			}
		}
		if !foundValidationLog {
			t.Error("expected NormalizationLog entry for ContentBlockValidation")
		}
	})
}
