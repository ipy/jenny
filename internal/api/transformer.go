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

// deepSeekAnthropicTransform injects DeepSeek-specific fields into Anthropic-format requests.
// DeepSeek's Anthropic-compatible endpoint uses output_config.effort for thinking effort.
// It ignores budget_tokens, so we map the effort level from the ThinkingConfig.
func deepSeekAnthropicTransform(body any) {
	req, ok := body.(*AnthropicRequest)
	if !ok {
		return
	}
	if req.Thinking != nil {
		req.OutputConfig = &OutputConfig{
			Effort: budgetToEffort(req.Thinking.BudgetTokens),
		}
	}
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

// budgetToEffort maps budget tokens to an effort string for DeepSeek's Anthropic endpoint.
// This is the inverse of effortToBudgetTokens.
func budgetToEffort(tokens int) string {
	switch {
	case tokens <= 4096:
		return "low"
	case tokens <= 16000:
		return "medium"
	case tokens <= 32000:
		return "high"
	default:
		return "max"
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
