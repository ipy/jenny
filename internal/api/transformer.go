package api

import "strings"

// isDeepSeekModel reports whether the model name indicates a DeepSeek model.
func isDeepSeekModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "deepseek")
}

// deepSeekOpenAITransform injects DeepSeek-specific fields into OpenAI-format requests.
//
// DeepSeek diverges from OpenAI's Chat Completions API in several ways:
//   - max_tokens field instead of max_completion_tokens
//   - reasoning_effort only supports "high" and "max" (low/medium → high)
//   - extra_body.thinking.type = "enabled" required to activate thinking mode
func deepSeekOpenAITransform(body any) {
	req, ok := body.(*OpenAIRequest)
	if !ok {
		return
	}

	// DeepSeek uses "max_tokens" not "max_completion_tokens".
	// Move the value to ExtraFields so MarshalJSON flattens it to the top level.
	if req.MaxCompletionTokens != nil {
		if req.ExtraFields == nil {
			req.ExtraFields = make(map[string]any)
		}
		req.ExtraFields["max_tokens"] = *req.MaxCompletionTokens
		req.MaxCompletionTokens = nil
	}

	// DeepSeek only supports "high" and "max" reasoning_effort.
	// Map unsupported levels (low, medium) to "high".
	if req.ReasoningEffort != "" {
		req.ReasoningEffort = normalizeDeepSeekEffort(req.ReasoningEffort)
		if req.ExtraBody == nil {
			req.ExtraBody = make(map[string]any)
		}
		req.ExtraBody["thinking"] = map[string]any{
			"type": "enabled",
		}
	}
}

// deepSeekAnthropicTransform adapts Anthropic-format requests for DeepSeek.
// DeepSeek uses a unified max_tokens budget (set via SetMaxTokensOverride) and
// does not support separate budget_tokens. This transform:
//  1. Copies the original --effort value to output_config.effort (static, preserves cache)
//  2. Removes the Thinking field (budget_tokens is Anthropic-only)
func deepSeekAnthropicTransform(body any) {
	req, ok := body.(*AnthropicRequest)
	if !ok {
		return
	}
	if req.Thinking != nil && req.Thinking.Effort != "" {
		req.OutputConfig = &OutputConfig{
			Effort: req.Thinking.Effort,
		}
	}
	req.Thinking = nil
}

// deepSeekOpenAIResponsesTransform injects DeepSeek-specific fields into
// OpenAI Responses-format requests.
//
// DeepSeek diverges from OpenAI's Responses API in several ways:
//   - Does not accept "developer" role; system prompts must use "system"
//   - max_output_tokens is accepted but reasoning_effort only supports "high"/"max"
//   - extra_body.thinking.type = "enabled" required to activate thinking mode
func deepSeekOpenAIResponsesTransform(body any) {
	req, ok := body.(*OpenAIResponsesRequest)
	if !ok {
		return
	}

	// DeepSeek does not accept "developer" role in input messages.
	// Rewrite any "developer" role items to "system".
	if input, ok := req.Input.([]any); ok {
		for _, item := range input {
			if m, ok := item.(map[string]any); ok {
				if role, ok := m["role"].(string); ok && role == "developer" {
					m["role"] = "system"
				}
			}
		}
	}

	// Normalize reasoning effort to DeepSeek's supported range.
	if req.ReasoningConfig != nil && req.ReasoningConfig.Effort != "" {
		req.ReasoningConfig.Effort = normalizeDeepSeekEffort(req.ReasoningConfig.Effort)
		req.ExtraBody = map[string]any{
			"thinking": map[string]any{
				"type": "enabled",
			},
		}
	}
}

// normalizeDeepSeekEffort maps reasoning effort to DeepSeek's supported range.
// DeepSeek only supports "high" and "max"; "low" and "medium" map to "high".
func normalizeDeepSeekEffort(effort string) string {
	switch effort {
	case "low", "medium":
		return "high"
	case "high", "max":
		return effort
	default:
		return "high"
	}
}

// resolveTransformer returns the appropriate RequestTransformer for a given model.
// Returns nil if no backend-specific transformations are needed.
func resolveTransformer(model string) RequestTransformer {
	if !isDeepSeekModel(model) {
		return nil
	}
	// DeepSeek needs different transforms depending on which provider format is used.
	// We return a dispatch function that checks the body type at call time.
	return func(body any) {
		switch body.(type) {
		case *OpenAIRequest:
			deepSeekOpenAITransform(body)
		case *AnthropicRequest:
			deepSeekAnthropicTransform(body)
		case *OpenAIResponsesRequest:
			deepSeekOpenAIResponsesTransform(body)
		}
	}
}
