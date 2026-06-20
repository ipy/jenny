package api

import "strings"

// isDeepSeekModel reports whether the model name indicates a DeepSeek model.
func isDeepSeekModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "deepseek")
}

// deepSeekOpenAITransform injects DeepSeek-specific fields into OpenAI-format requests.
// DeepSeek requires extra_body.thinking.type = "enabled" to activate thinking mode.
func deepSeekOpenAITransform(body any) {
	req, ok := body.(*OpenAIRequest)
	if !ok {
		return
	}
	if req.ReasoningEffort != "" {
		req.ExtraBody = map[string]any{
			"thinking": map[string]any{
				"type": "enabled",
			},
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
		}
	}
}
