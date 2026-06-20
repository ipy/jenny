package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// TestOpenAIResponsesProvider_DeepSeekTransformer verifies that DeepSeek models
// routed through OpenAI Responses API get extra_body.thinking.type=enabled
// when reasoning effort is configured. Regression test for the transformer
// wiring gap described in provider_openai_responses.go.
// AC: DeepSeek transformer is applied to Responses-format requests.
func TestOpenAIResponsesProvider_DeepSeekTransformer(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		if req["model"] != "deepseek-v4-pro" {
			t.Errorf("expected model 'deepseek-v4-pro', got %v", req["model"])
		}

		// Verify extra_body.thinking.type is "enabled"
		extraBody, ok := req["extra_body"].(map[string]any)
		if !ok {
			t.Fatal("expected extra_body in request (DeepSeek transformer not applied)")
		}
		thinking, ok := extraBody["thinking"].(map[string]any)
		if !ok {
			t.Fatal("expected extra_body.thinking in request")
		}
		if thinking["type"] != "enabled" {
			t.Errorf("expected thinking.type 'enabled', got %v", thinking["type"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "deepseek-v4-pro",
			"output": [{
				"id": "msg_1", "type": "message", "role": "assistant",
				"content": [{"type": "output_text", "text": "Here is the plan."}]
			}],
			"usage": {"input_tokens": 10, "output_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "deepseek-v4-pro")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_WIRE_API", "responses")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	client.SetThinkingConfig(ThinkingConfig{Effort: "medium"})

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Plan it."}}, nil, nil, []string{}, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", resp.StopReason)
	}
}

// TestOpenAIResponsesProvider_SupportsNativeSearch verifies that SupportsNativeSearch
// returns true unconditionally for the OpenAI Responses API provider.
func TestOpenAIResponsesProvider_SupportsNativeSearch(t *testing.T) {
	tests := []struct {
		model string
	}{
		{model: "gpt-4o"},
		{model: "gpt-5"},
		{model: "o3"},
		{model: ""},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := &openAIResponsesProvider{model: tt.model}
			if !p.SupportsNativeSearch() {
				t.Errorf("SupportsNativeSearch() for model %q = false, want true", tt.model)
			}
		})
	}
}

// TestOpenAIResponsesProvider_FunctionCallNullDelta verifies that a
// response.function_call_arguments.delta with the literal string "null"
// is suppressed in the stream — the final args normalize to an empty map
// and emitting "null" as a partial JSON delta would confuse consumers.
func TestOpenAIResponsesProvider_FunctionCallNullDelta(t *testing.T) {
	p := &openAIResponsesProvider{model: "gpt-4o"}

	acc := newResponsesStreamAccumulator()
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	// Simulate: output_item.added for a function_call
	p.processResponsesStreamEvent(&OpenAIResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &OpenAIResponsesOutputItem{Type: "function_call", Name: "noop", ID: "fc_1"},
	}, acc, blocksChan, result)

	// Simulate: function_call_arguments.delta with literal "null"
	completed := p.processResponsesStreamEvent(&OpenAIResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 0,
		Delta:       "null",
	}, acc, blocksChan, result)

	if completed {
		t.Error("expected not completed after null delta")
	}

	// Simulate: output_item.done
	p.processResponsesStreamEvent(&OpenAIResponsesStreamEvent{
		Type:        "response.output_item.done",
		OutputIndex: 0,
		Item:        &OpenAIResponsesOutputItem{Type: "function_call", Name: "noop", ID: "fc_1"},
	}, acc, blocksChan, result)

	close(blocksChan)

	// Verify stream did NOT emit a null PartialJSON delta
	for ev := range blocksChan {
		raw, ok := ev.RawEvent.(AnthropicStreamEvent)
		if !ok {
			continue
		}
		if raw.Type == EventContentBlockDelta && raw.Delta != nil && raw.Delta.PartialJSON == "null" {
			t.Error("stream emitted PartialJSON 'null' for no-arg function_call")
		}
	}

	// Finalized blocks should have empty map for the tool input
	blocks := acc.finalize()
	for _, b := range blocks {
		if b.Type == BlockTypeToolUse {
			if len(b.ToolInput) != 0 {
				t.Errorf("expected empty tool input ({}), got %v", b.ToolInput)
			}
		}
	}
}
