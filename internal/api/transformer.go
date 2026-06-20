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

// deepSeekOpenAIResponsesTransform injects DeepSeek-specific fields into
// OpenAI Responses-format requests. DeepSeek's OpenAI-compatible endpoint
// accepts the same extra_body.thinking.type=enabled extension when
// reasoning is requested, so when a reasoning config is present we mirror
// the Chat path: emit extra_body.thinking.type=enabled. This is what
// DeepSeek returns to switch the model into its native chain-of-thought
// mode regardless of which OpenAI wire format is in use.
func deepSeekOpenAIResponsesTransform(body any) {
	req, ok := body.(*OpenAIResponsesRequest)
	if !ok {
		return
	}
	if req.ReasoningConfig != nil && req.ReasoningConfig.Effort != "" {
		req.ExtraBody = map[string]any{
			"thinking": map[string]any{
				"type": "enabled",
			},
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
		case *OpenAIResponsesRequest:
			deepSeekOpenAIResponsesTransform(body)
		}
	}
}
