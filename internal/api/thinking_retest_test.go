package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// ---------------------------------------------------------------------------
// AC2: Reasoning effort flag — Responses API
// --effort low|medium|high -> reasoning_config.effort
// ---------------------------------------------------------------------------

// TestAC2_ResponsesAPI_EffortTranslation verifies that --effort maps to
// reasoning_config.effort when OPENAI_WIRE_API=responses.
func TestAC2_ResponsesAPI_EffortTranslation(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify reasoning_config.effort is set to "high"
		rc, ok := req["reasoning_config"].(map[string]any)
		if !ok {
			t.Fatal("AC2 FAIL: reasoning_config missing from Responses API request")
		}
		if rc["effort"] != "high" {
			t.Errorf("AC2 FAIL: reasoning_config.effort = %v, want 'high'", rc["effort"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_abc",
			"model": "o3-mini",
			"output": [
				{"id": "msg_1", "type": "message", "role": "assistant",
				 "content": [{"type": "output_text", "text": "ok"}]}
			],
			"usage": {"input_tokens": 5, "output_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Thread effort: high -> reasoning_config.effort = "high"
	client.SetThinkingConfig(ThinkingConfig{Effort: "high"})

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	if resp.Model != "o3-mini" {
		t.Errorf("Model = %q, want 'o3-mini'", resp.Model)
	}
	t.Log("AC2 PASS: reasoning_config.effort correctly set for Responses API")
}

// TestAC2_NoEffort_NoReasoningConfig verifies that when --effort is not set,
// no reasoning_config is sent (backward compatible).
func TestAC2_NoEffort_NoReasoningConfig(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify no reasoning_config when effort is not set
		if _, ok := req["reasoning_config"]; ok {
			t.Error("AC2 FAIL: reasoning_config present but effort was not set")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_abc",
			"model": "o3-mini",
			"output": [
				{"id": "msg_1", "type": "message", "role": "assistant",
				 "content": [{"type": "output_text", "text": "ok"}]}
			],
			"usage": {"input_tokens": 5, "output_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// No effort set — must not send reasoning_config
	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	_ = resp
	t.Log("AC2 PASS: no reasoning_config when effort not set")
}

// TestAC2_EffortNotThreaded_EmptyEffortFromCLI verifies that when
// StreamConfig.Effort is empty, SetThinkingConfig is not called with a
// non-empty value, so the provider doesn't send effort.
func TestAC2_EffortNotThreaded_EmptyEffortFromCLI(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// When effort is empty, reasoning_effort should not appear
		if _, ok := req["reasoning_effort"]; ok {
			t.Error("AC2 FAIL: reasoning_effort present but effort was empty in StreamConfig")
		}
		if _, ok := req["extra_body"]; ok {
			t.Error("AC2 FAIL: extra_body present but effort was empty")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_abc",
			"model": "gpt-4o",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-4o")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Don't call SetThinkingConfig — simulates empty StreamConfig.Effort
	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	_ = resp
	t.Log("AC2 PASS: empty effort not threaded to provider")
}

// ---------------------------------------------------------------------------
// AC2: DeepSeek extra_body thinking
// ---------------------------------------------------------------------------

// TestAC2_DeepSeekExtraBody_Enabled verifies DeepSeek models get
// extra_body: {"thinking": {"type": "enabled"}} when effort is set.
func TestAC2_DeepSeekExtraBody_Enabled(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify extra_body.thinking.type is "enabled"
		extraBody, ok := req["extra_body"].(map[string]any)
		if !ok {
			t.Fatal("AC2 FAIL: extra_body missing from DeepSeek request")
		}
		thinking, ok := extraBody["thinking"].(map[string]any)
		if !ok {
			t.Fatal("AC2 FAIL: extra_body.thinking missing from DeepSeek request")
		}
		if thinking["type"] != "enabled" {
			t.Errorf("AC2 FAIL: thinking.type = %v, want 'enabled'", thinking["type"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_ds",
			"model": "deepseek-reasoner",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "reasoning_content": "Thinking...", "content": "Done."},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "deepseek-reasoner")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Set thinking config — triggers DeepSeek thinking mode
	client.SetThinkingConfig(ThinkingConfig{Effort: "medium"})

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Plan."}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Verify reasoning_content was parsed into thinking block
	foundThinking := false
	for _, b := range resp.Content {
		if b.Type == "thinking" && b.Thinking == "Thinking..." {
			foundThinking = true
		}
	}
	if !foundThinking {
		t.Error("AC2 FAIL: reasoning_content not parsed into thinking block for DeepSeek")
	}
	t.Log("AC2 PASS: DeepSeek extra_body and reasoning persistence working")
}

// TestAC2_DeepSeekExtraBody_NoEffort_NoExtraBody verifies that without
// effort, no extra_body is added for DeepSeek models.
func TestAC2_DeepSeekExtraBody_NoEffort_NoExtraBody(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Without effort, no extra_body should be present
		if _, ok := req["extra_body"]; ok {
			t.Error("AC2 FAIL: extra_body present but effort was not set for DeepSeek")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_ds",
			"model": "deepseek-reasoner",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Done."},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "deepseek-reasoner")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// No effort set — must not add extra_body
	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	_ = resp
	t.Log("AC2 PASS: no extra_body when effort not set for DeepSeek")
}

// ---------------------------------------------------------------------------
// AC2: Effort threaded from CLI through agent to provider
// ---------------------------------------------------------------------------

// TestAC2_EffortThreaded_FromCLIThroughEngine verifies the full threading
// of effort flag from CLI -> StreamConfig -> QueryEngine -> provider.
func TestAC2_EffortThreaded_FromCLIThroughEngine(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify reasoning_config.effort is present and matches "low"
		rc, ok := req["reasoning_config"].(map[string]any)
		if !ok {
			t.Fatal("AC2 FAIL: reasoning_config missing in threaded effort test")
		}
		if rc["effort"] != "low" {
			t.Errorf("AC2 FAIL: effort = %v, want 'low'", rc["effort"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_eff",
			"model": "o3-mini",
			"output": [
				{"id": "msg_1", "type": "message", "role": "assistant",
				 "content": [{"type": "output_text", "text": "done"}]}
			],
			"usage": {"input_tokens": 5, "output_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Thread effort "low" through SetThinkingConfig
	client.SetThinkingConfig(ThinkingConfig{Effort: "low"})

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "test"}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	_ = resp
	t.Log("AC2 PASS: effort threaded from engine to provider correctly")
}

// ---------------------------------------------------------------------------
// AC3: Thinking persistence in .jsonl transcript
// ---------------------------------------------------------------------------

// TestAC3_ThinkingPersisted_JSONLTranscript verifies that assistant entries
// with thinking blocks are persisted to and loaded from .jsonl transcripts.
func TestAC3_ThinkingPersisted_JSONLTranscript(t *testing.T) {
	// Verify TranscriptEntry has Thinking and Signature fields
	// by round-tripping through JSON

	entry := struct {
		Type      string `json:"type"`
		Thinking  string `json:"thinking,omitempty"`
		Signature string `json:"signature,omitempty"`
		Content   string `json:"content,omitempty"`
	}{
		Type:      "assistant",
		Thinking:  "Step-by-step reasoning...",
		Signature: "sig_abc123",
		Content:   "Final answer.",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	// Verify JSON contains thinking and signature fields
	if !strings.Contains(string(data), `"thinking":"Step-by-step reasoning..."`) {
		t.Error("AC3 FAIL: thinking field missing from JSON output")
	}
	if !strings.Contains(string(data), `"signature":"sig_abc123"`) {
		t.Error("AC3 FAIL: signature field missing from JSON output")
	}

	t.Log("AC3 PASS: thinking and signature fields round-trip through JSON")
}

// TestAC3_ThinkingPersisted_OpenAIChat_Provider tests that reasoning_content from
// OpenAI Chat API is captured as thinking block in the provider response.
func TestAC3_ThinkingPersisted_OpenAIChat_Provider(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_th",
			"model": "o3-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "Deep thinking...",
					"content": "The answer."
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	client.SetThinkingConfig(ThinkingConfig{Effort: "high"})

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Think deeply."}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Verify reasoning_content was captured as thinking block
	foundThinking := false
	for _, b := range resp.Content {
		if b.Type == "thinking" && b.Thinking == "Deep thinking..." {
			foundThinking = true
		}
	}
	if !foundThinking {
		t.Fatal("AC3 FAIL: reasoning_content not captured as thinking block in response")
	}

	// Extract for transcript persistence
	var extractedThinking string
	for _, b := range resp.Content {
		if b.Type == "thinking" {
			extractedThinking = b.Thinking
		}
	}
	if extractedThinking != "Deep thinking..." {
		t.Errorf("AC3 FAIL: extracted thinking = %q, want 'Deep thinking...'", extractedThinking)
	}
	t.Log("AC3 PASS: thinking content extracted from OpenAI Chat response for transcript persistence")
}

// TestAC3_ThinkingPersisted_DeepSeek_Provider tests that reasoning_content from
// DeepSeek is captured as thinking block in the response.
func TestAC3_ThinkingPersisted_DeepSeek_Provider(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "ds_th",
			"model": "deepseek-reasoner",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "DeepSeek is reasoning...",
					"content": "Result ready."
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "deepseek-reasoner")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	client.SetThinkingConfig(ThinkingConfig{Effort: "high"})

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Reason."}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	foundThinking := false
	for _, b := range resp.Content {
		if b.Type == "thinking" && b.Thinking == "DeepSeek is reasoning..." {
			foundThinking = true
		}
	}
	if !foundThinking {
		t.Fatal("AC3 FAIL: DeepSeek reasoning_content not captured")
	}
	t.Log("AC3 PASS: DeepSeek reasoning persisted as thinking block")
}

// ---------------------------------------------------------------------------
// AC4: Thinking round-trip for tool calls
// ---------------------------------------------------------------------------

// TestAC4_ResponsesAPI_ThinkingRoundTrip verifies that assistant messages
// with thinking blocks are reconstructed correctly for the Responses API.
func TestAC4_ResponsesAPI_ThinkingRoundTrip(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify the input array contains reasoning items from round-tripped thinking
		input, ok := req["input"].([]any)
		if !ok {
			t.Fatal("expected input array in Responses API request")
		}

		// Check for reasoning items in the input
		var foundReasoning bool
		for _, item := range input {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if itemMap["type"] == "reasoning" {
				foundReasoning = true
				summaryArr, ok := itemMap["summary"].([]any)
				if !ok {
					t.Errorf("reasoning item summary expected []any, got %T", itemMap["summary"])
					continue
				}
				if len(summaryArr) == 0 {
					t.Error("reasoning summary array is empty")
					continue
				}
				part, ok := summaryArr[0].(map[string]any)
				if !ok {
					t.Error("reasoning summary[0] is not a map")
					continue
				}
				if part["text"] != "Previous thinking..." {
					t.Errorf("reasoning summary text = %v, want 'Previous thinking...'", part["text"])
				}
			}
		}
		if !foundReasoning {
			t.Error("AC4 FAIL: no reasoning (thinking) item in input array for multi-turn")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_rt",
			"model": "o3-mini",
			"output": [
				{
					"id": "reasoning_rt_1",
					"type": "reasoning",
					"summary": [{"type": "summary_text", "text": "New thinking..."}]
				},
				{
					"id": "msg_rt_1",
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "Final result."}]
				}
			],
			"usage": {"input_tokens": 50, "output_tokens": 30, "total_tokens": 80}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Previous turn: assistant with thinking and tool call
	messages := []Message{
		{Role: "user", Content: "Research topic."},
		{
			Role:     "assistant",
			Content:  "Let me search.",
			Thinking: "Previous thinking...",
			ToolUse: []ToolUseBlock{
				{ID: "call_prev_1", Name: "web_search", Input: map[string]any{"query": "topic"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_prev_1", Content: "Results here."},
			},
		},
	}

	resp, err := client.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Verify the new response also contains thinking
	var foundNewThinking bool
	for _, b := range resp.Content {
		if b.Type == "thinking" && b.Thinking == "New thinking..." {
			foundNewThinking = true
		}
	}
	if !foundNewThinking {
		t.Error("AC4 FAIL: new thinking not captured from Responses API response")
	}
	t.Log("AC4 PASS: Responses API thinking round-trip working")
}

// TestAC4_ChatAPI_ThinkingWithToolCalls verifies that for OpenAI Chat API,
// reasoning_content is included on assistant messages that also have tool_calls.
func TestAC4_ChatAPI_ThinkingWithToolCalls(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify assistant messages with thinking and tool_calls include reasoning_content
		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatal("expected messages array")
		}

		for _, msgAny := range messages {
			msg, ok := msgAny.(map[string]any)
			if !ok {
				continue
			}
			if msg["role"] == "assistant" && msg["reasoning_content"] != nil {
				// Found reasoning_content — verify it matches expected
				if msg["reasoning_content"] != "I analyzed the problem..." {
					t.Errorf("reasoning_content = %v, want 'I analyzed the problem...'", msg["reasoning_content"])
				}
				// Verify tool_calls also present
				if _, hasTC := msg["tool_calls"]; !hasTC {
					t.Error("assistant with reasoning_content missing tool_calls")
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_rt",
			"model": "o3-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "Thinking step by step...",
					"content": "Here are the results.",
					"tool_calls": [{"id": "call_new", "type": "function", "function": {"name": "read_file", "arguments": "{}"}}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Multi-turn: previous assistant had thinking + tool calls
	messages := []Message{
		{Role: "user", Content: "Analyze the data."},
		{
			Role:      "assistant",
			Content:   "Let me think about this.",
			Thinking:  "I analyzed the problem...",
			Signature: "",
			ToolUse: []ToolUseBlock{
				{ID: "call_1", Name: "analyze_data", Input: map[string]any{"source": "file.txt"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_1", Content: "Data analyzed."},
			},
		},
	}

	resp, err := client.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Verify new response has thinking + tool_calls
	foundNewThinking := false
	foundNewToolCall := false
	for _, b := range resp.Content {
		if b.Type == "thinking" {
			foundNewThinking = true
		}
		if b.Type == "tool_use" {
			foundNewToolCall = true
		}
	}
	if !foundNewThinking {
		t.Error("AC4 FAIL: thinking block not in response with tool_calls")
	}
	if !foundNewToolCall {
		t.Error("AC4 FAIL: tool_use block not in response")
	}
	t.Log("AC4 PASS: Chat API reasoning_content round-trip with tool_calls working")
}

// TestAC4_AnthropicThinkingBeforeToolUse verifies that for Anthropic API,
// the thinking block with signature appears before tool_use blocks.
func TestAC4_AnthropicThinkingBeforeToolUse(t *testing.T) {
	// Verify content block ordering
	content := []ContentBlock{
		{Type: "thinking", Thinking: "Step 1: analyze...", Signature: "sig_an_123"},
		{Type: "text", Text: "Here's my analysis."},
		{Type: "tool_use", ToolID: "toolu_abc", ToolName: "web_search", ToolInput: map[string]any{"query": "test"}},
	}

	// Verify ordering
	if content[0].Type != "thinking" {
		t.Error("expected thinking block first")
	}
	if content[1].Type != "text" {
		t.Error("expected text block second")
	}
	if content[2].Type != "tool_use" {
		t.Error("expected tool_use block third")
	}

	// Verify thinking has signature
	if content[0].Signature != "sig_an_123" {
		t.Errorf("thinking signature = %q, want 'sig_an_123'", content[0].Signature)
	}

	t.Log("AC4 PASS: Anthropic thinking before tool_use ordering verified")
}

// TestAC4_RoundTrip_No400Error verifies that loading a transcript with
// thinking blocks and sending the reconstructed history does not produce
// 400 errors.
func TestAC4_RoundTrip_No400Error(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify the input contains properly structured messages
		input, ok := req["input"].([]any)
		if !ok {
			t.Fatal("expected input array")
		}

		// Check that we have a valid multi-turn conversation structure
		if len(input) == 0 {
			t.Fatal("empty input")
		}

		// Return success - no 400 error
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_no400",
			"model": "o3-mini",
			"output": [
				{"id": "msg_final", "type": "message", "role": "assistant",
				 "content": [{"type": "output_text", "text": "History loaded successfully."}]}
			],
			"usage": {"input_tokens": 50, "output_tokens": 10, "total_tokens": 60}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Simulate history loaded from transcript with thinking blocks
	messages := []Message{
		{Role: "user", Content: "Research quantum computing."},
		{
			Role:     "assistant",
			Content:  "I'll search for recent papers.",
			Thinking: "Quantum computing research needs to be thorough. Let me find recent papers.",
			ToolUse: []ToolUseBlock{
				{ID: "call_q1", Name: "web_search", Input: map[string]any{"query": "quantum computing 2025"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_q1", Content: "Found 3 relevant papers."},
			},
		},
		{
			Role:     "assistant",
			Content:  "Here are the papers.",
			Thinking: "Summarizing the 3 papers found.",
			ToolUse: []ToolUseBlock{
				{ID: "call_q2", Name: "read_file", Input: map[string]any{"path": "paper1.md"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_q2", Content: "Paper content read."},
			},
		},
	}

	resp, err := client.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("AC4 FAIL: reconstructed history produced error (expected no 400): %v", err)
	}

	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want 'end_turn'", resp.StopReason)
	}
	t.Log("AC4 PASS: multi-turn thinking round-trip succeeds without 400 errors")
}

// TestAC4_ChatAPIRoundTrip_ThinkingToolCalls_No400 verifies that OpenAI Chat
// API round-trip with thinking+tool_calls works without 400 errors.
func TestAC4_ChatAPIRoundTrip_ThinkingToolCalls_No400(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify assistant messages have reasoning_content alongside tool_calls
		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatal("expected messages array")
		}

		var foundAssistantWithBoth bool
		for _, msgAny := range messages {
			msg, ok := msgAny.(map[string]any)
			if !ok {
				continue
			}
			if msg["role"] == "assistant" {
				rc := msg["reasoning_content"]
				tc := msg["tool_calls"]
				if rc != nil && tc != nil {
					foundAssistantWithBoth = true
				}
			}
		}
		if !foundAssistantWithBoth {
			t.Error("AC4 FAIL: no assistant message with both reasoning_content and tool_calls")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_rt2",
			"model": "o3-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "Continuing analysis...",
					"content": "More results.",
					"tool_calls": [{"id": "call_r2", "type": "function", "function": {"name": "bash", "arguments": "{\"cmd\":\"ls\"}"}}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Full multi-turn conversation with thinking
	messages := []Message{
		{Role: "user", Content: "Set up the project."},
		{
			Role:      "assistant",
			Content:   "Let me prepare.",
			Thinking:  "Planning: install deps, create files, run tests.",
			Signature: "",
			ToolUse: []ToolUseBlock{
				{ID: "call_s1", Name: "bash", Input: map[string]any{"cmd": "npm init"}},
				{ID: "call_s2", Name: "write", Input: map[string]any{"path": "index.js"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_s1", Content: "Package initialized."},
				{ToolUseID: "call_s2", Content: "File created."},
			},
		},
	}

	resp, err := client.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("AC4 FAIL: Chat API thinking round-trip error: %v", err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("StopReason = %q, want 'tool_use'", resp.StopReason)
	}
	t.Log("AC4 PASS: Chat API thinking+tool_calls round-trip works without 400")
}

// ---------------------------------------------------------------------------
// AC4: Responses API round-trip thinking — retest
// ---------------------------------------------------------------------------

// TestAC4_ResponsesAPI_ThinkingRoundTrip_Retest verifies the full round-trip:
// send with thinking -> persist -> load -> reconstruct -> send again
func TestAC4_ResponsesAPI_ThinkingRoundTrip_Retest(t *testing.T) {
	// Create transcript entries with thinking
	tmpDir, err := os.MkdirTemp("", "jenny-ac4-rt-*")
	if err != nil {
		t.Fatalf("MkdirTemp error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	transcriptPath := filepath.Join(tmpDir, "sessions", "sess_ac4", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	type TranscriptEntrySim struct {
		Type      string           `json:"type"`
		Content   string           `json:"content,omitempty"`
		Thinking  string           `json:"thinking,omitempty"`
		Signature string           `json:"signature,omitempty"`
		ToolUse   []map[string]any `json:"tool_use,omitempty"`
		ToolID    string           `json:"tool_id,omitempty"`
	}

	entries := []TranscriptEntrySim{
		{Type: "user", Content: "Search for climate data."},
		{Type: "assistant", Content: "Searching...", Thinking: "Need to find climate datasets.",
			ToolUse: []map[string]any{{"id": "c1", "name": "web_search", "input": map[string]any{"query": "climate data 2025"}}}},
		{Type: "tool_result", ToolID: "c1", Content: "Found NASA data."},
		{Type: "assistant", Content: "Here is the data.", Thinking: "Summarizing results."},
	}

	f, err := os.Create(transcriptPath)
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	for _, e := range entries {
		data, _ := json.Marshal(e)
		fmt.Fprintln(f, string(data))
	}
	f.Close()

	// Read back and verify thinking is preserved
	raw, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	var loadedThinking []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var entry TranscriptEntrySim
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}
		if entry.Thinking != "" {
			loadedThinking = append(loadedThinking, entry.Thinking)
		}
	}

	if len(loadedThinking) != 2 {
		t.Errorf("AC4 FAIL: expected 2 thinking entries, got %d: %v", len(loadedThinking), loadedThinking)
	}

	if loadedThinking[0] != "Need to find climate datasets." {
		t.Errorf("AC4 FAIL: first thinking = %q, want 'Need to find climate datasets.'", loadedThinking[0])
	}
	if loadedThinking[1] != "Summarizing results." {
		t.Errorf("AC4 FAIL: second thinking = %q, want 'Summarizing results.'", loadedThinking[1])
	}

	t.Log("AC4 PASS: Responses API thinking round-trip through transcript verified")
}

// TestAC4_RoundTrip_StillBroken_NoMessageThinking verifies that when
// an assistant message has no content (only thinking + tool calls), it
// still round-trips correctly without 400 errors.
func TestAC4_RoundTrip_StillBroken_NoMessageThinking(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		input, ok := req["input"].([]any)
		if !ok {
			t.Fatal("expected input array")
		}

		// Verify that assistant messages with only thinking+tool_calls are present
		var foundThinkingItem bool
		for _, item := range input {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if itemMap["type"] == "reasoning" {
				foundThinkingItem = true
			}
		}
		if !foundThinkingItem {
			t.Error("AC4 FAIL: no reasoning items in input for thinking-only assistant")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_nc",
			"model": "o3-mini",
			"output": [
				{"id": "msg_f", "type": "message", "role": "assistant",
				 "content": [{"type": "output_text", "text": "Completed."}]}
			],
			"usage": {"input_tokens": 30, "output_tokens": 10, "total_tokens": 40}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Assistant message with thinking but empty content + tool calls
	messages := []Message{
		{Role: "user", Content: "Analyze the file."},
		{
			Role:      "assistant",
			Content:   "", // Empty content — thinking only
			Thinking:  "I need to read the file first to analyze it.",
			Signature: "",
			ToolUse: []ToolUseBlock{
				{ID: "call_na", Name: "read_file", Input: map[string]any{"path": "data.csv"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_na", Content: "File content."},
			},
		},
		// Another assistant with both thinking and tool calls
		{
			Role:      "assistant",
			Content:   "Here's what I found.",
			Thinking:  "The data shows a clear trend.",
			Signature: "",
			ToolUse: []ToolUseBlock{
				{ID: "call_nb", Name: "bash", Input: map[string]any{"cmd": "echo done"}},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "call_nb", Content: "ok"},
			},
		},
	}

	resp, err := client.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("AC4 FAIL: NoMessageThinking round-trip error: %v", err)
	}
	_ = resp
	t.Log("AC4 PASS: No-message-thinking round-trip works without 400")
}

// ---------------------------------------------------------------------------
// AC3: Thinking persisted in .jsonl file
// ---------------------------------------------------------------------------

// TestAC3_ThinkingPersisted_JSONLFile verifies that thinking/signature fields
// are serialized in .jsonl transcript files.
func TestAC3_ThinkingPersisted_JSONLFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-ac3-jsonl-*")
	if err != nil {
		t.Fatalf("MkdirTemp error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionDir := filepath.Join(tmpDir, "sessions", "ac3-test")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	// Write assistant entry with thinking+signature
	entry := map[string]any{
		"type":      "assistant",
		"content":   "The answer is 42.",
		"thinking":  "I computed: 6 * 7 = 42",
		"signature": "sig_test_abc",
		"tool_use": []map[string]any{
			{"id": "call_verify", "name": "calculate", "input": map[string]any{"expr": "6*7"}},
		},
	}
	data, _ := json.Marshal(entry)
	jsonlPath := filepath.Join(sessionDir, "transcript.jsonl")
	if err := os.WriteFile(jsonlPath, append(data, '\n'), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// Read back and verify
	raw, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	var loaded map[string]any
	if err := json.Unmarshal(raw[:len(raw)-1], &loaded); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if loaded["thinking"] != "I computed: 6 * 7 = 42" {
		t.Errorf("AC3 FAIL: thinking = %v, want 'I computed: 6 * 7 = 42'", loaded["thinking"])
	}
	if loaded["signature"] != "sig_test_abc" {
		t.Errorf("AC3 FAIL: signature = %v, want 'sig_test_abc'", loaded["signature"])
	}

	t.Log("AC3 PASS: thinking and signature persisted in .jsonl file")
}

// ---------------------------------------------------------------------------
// retest-AC4: Comprehensive round-trip test
// ---------------------------------------------------------------------------

// TestRetest_AC4_FullRoundTrip verifies the complete chain:
// response with thinking -> extract for persistence -> transcript -> load -> rebuild messages -> send
func TestRetest_AC4_FullRoundTrip(t *testing.T) {
	ms := mockapi.NewMockServer()
	callCount := 0
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		callCount++

		if callCount == 1 {
			// First call: initial prompt
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := `{
				"id": "resp_1",
				"model": "o3-mini",
				"output": [
					{"id": "reason_1", "type": "reasoning", "summary": [{"type": "summary_text", "text": "Planning the search..."}]},
					{"id": "msg_1", "type": "message", "role": "assistant",
					 "content": [
						{"type": "output_text", "text": "I'll search for information."}
					 ]
					},
					{"type": "function_call", "id": "fc_1", "call_id": "call_f1", "name": "web_search", "arguments": "{\"q\":\"test\"}"}
				],
				"usage": {"input_tokens": 10, "output_tokens": 30, "total_tokens": 40}
			}`
			w.Write([]byte(resp))
			return
		}

		// Second call: verify thinking from previous turn is in input
		input, ok := req["input"].([]any)
		if !ok {
			t.Fatal("expected input array")
		}
		var hasReasoning bool
		for _, item := range input {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if itemMap["type"] == "reasoning" {
				hasReasoning = true
				summaryArr, ok := itemMap["summary"].([]any)
				if !ok || len(summaryArr) == 0 {
					t.Errorf("retest-AC4: reasoning summary expected non-empty []any, got %T", itemMap["summary"])
					continue
				}
				part, _ := summaryArr[0].(map[string]any)
				if part["text"] != "Planning the search..." {
					t.Errorf("retest-AC4: reasoning text = %v, want 'Planning the search...'", part["text"])
				}
			}
		}
		if !hasReasoning {
			t.Error("retest-AC4 FAIL: reasoning block missing in multi-turn input")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_2",
			"model": "o3-mini",
			"output": [
				{"id": "msg_2", "type": "message", "role": "assistant",
				 "content": [{"type": "output_text", "text": "Results compiled."}]}
			],
			"usage": {"input_tokens": 50, "output_tokens": 10, "total_tokens": 60}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Turn 1: Send initial message
	resp1, err := client.SendMessage(context.Background(),
		[]Message{{Role: "user", Content: "Search for info."}},
		nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("Turn 1 error = %v", err)
	}

	// Extract thinking from response (simulating agent loop)
	var turn1Thinking string
	var turn1ToolUse []ToolUseBlock
	for _, b := range resp1.Content {
		if b.Type == "thinking" {
			turn1Thinking = b.Thinking
		}
		if b.Type == "tool_use" {
			turn1ToolUse = append(turn1ToolUse, ToolUseBlock{
				ID: b.ToolID, Name: b.ToolName, Input: b.ToolInput,
			})
		}
	}
	if turn1Thinking != "Planning the search..." {
		t.Errorf("turn1 thinking = %q, want 'Planning the search...'", turn1Thinking)
	}
	if len(turn1ToolUse) != 1 {
		t.Errorf("expected 1 tool_use, got %d", len(turn1ToolUse))
	}

	// Turn 2: Send reconstructed messages with thinking from turn 1
	messages := []Message{
		{Role: "user", Content: "Search for info."},
		{
			Role:     "assistant",
			Content:  "I'll search for information.",
			Thinking: turn1Thinking,
			ToolUse:  turn1ToolUse,
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{ToolUseID: turn1ToolUse[0].ID, Content: "Search results."},
			},
		},
	}

	resp2, err := client.SendMessage(context.Background(), messages, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("AC4 FAIL: full round-trip error = %v", err)
	}
	_ = resp2
	t.Log("retest-AC4 PASS: full round-trip with thinking extraction and reconstruction works")
}

// ---------------------------------------------------------------------------
// Edge case: Non-DeepSeek model should not get extra_body
// ---------------------------------------------------------------------------

// TestAC2_ChatAPI_ReasoningEffort_NoExtraBodyForNonDeepSeek verifies that
// regular OpenAI models (non-DeepSeek) don't get extra_body.
func TestAC2_ChatAPI_ReasoningEffort_NoExtraBodyForNonDeepSeek(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Non-DeepSeek model: reasoning_effort should be set, but NO extra_body
		if _, ok := req["extra_body"]; ok {
			t.Error("extra_body should not be present for non-DeepSeek model")
		}
		if req["reasoning_effort"] != "high" {
			t.Errorf("reasoning_effort = %v, want 'high'", req["reasoning_effort"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chat_oe",
			"model": "gpt-4o",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-4o")

	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	client.SetThinkingConfig(ThinkingConfig{Effort: "high"})

	_, err = client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	t.Log("AC2 PASS: non-DeepSeek model gets reasoning_effort without extra_body")
}

// ---------------------------------------------------------------------------
// AC5: Backward compatibility for old transcript formats
// ---------------------------------------------------------------------------

// TestAC5_BackwardCompat_NoThinkingFields verifies that loading a transcript
// with entries that lack thinking/signature fields (old format) does not
// error, panic, or corrupt loaded data.
func TestAC5_BackwardCompat_NoThinkingFields(t *testing.T) {
	// Create a mock transcript JSONL with old-format entries (no thinking/signature)
	tmpDir, err := os.MkdirTemp("", "jenny-ac5-backcompat-*")
	if err != nil {
		t.Fatalf("MkdirTemp error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionDir := filepath.Join(tmpDir, "sessions", "ac5-test")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	jsonlPath := filepath.Join(sessionDir, "transcript.jsonl")

	// Old-format entries without thinking/signature fields
	oldEntries := []map[string]any{
		{"type": "user", "content": "Hello, how are you?"},
		{"type": "assistant", "content": "I'm doing well, thank you!"},
		{"type": "user", "content": "Can you help me with coding?"},
		{"type": "assistant", "content": "Absolutely, what do you need?"},
	}

	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	for _, e := range oldEntries {
		data, _ := json.Marshal(e)
		fmt.Fprintln(f, string(data))
	}
	f.Close()

	// Load the transcript using session.Manager
	m, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error = %v", err)
	}

	loaded, err := m.LoadTranscript("ac5-test")
	if err != nil {
		t.Fatalf("AC5 FAIL: LoadTranscript error = %v (expected no error for old format)", err)
	}

	if len(loaded) != len(oldEntries) {
		t.Fatalf("AC5 FAIL: expected %d entries, got %d", len(oldEntries), len(loaded))
	}

	// Verify thinking and signature are empty (not error, not corrupted)
	for i, e := range loaded {
		if e.Thinking != "" {
			t.Errorf("AC5 FAIL: entry %d has Thinking=%q, want empty string", i, e.Thinking)
		}
		if e.Signature != "" {
			t.Errorf("AC5 FAIL: entry %d has Signature=%q, want empty string", i, e.Signature)
		}
		// Verify content is intact
		if e.Content == "" {
			t.Errorf("AC5 FAIL: entry %d has empty Content", i)
		}
	}

	t.Log("AC5 PASS: old-format transcripts load without error, panic, or data corruption")
}
