package api

import (
	"testing"
)

func TestClassifyErrorCommon(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       ErrorCategory
	}{
		{"413 context exhausted", 413, "", CategoryContextExhausted},
		{"402 payment required", 402, "", CategoryPaymentRequired},
		{"400 prompt too long", 400, `{"error": {"message": "prompt_too_long"}}`, CategoryContextExhausted},
		{"400 context length exceeded", 400, "context_length_exceeded", CategoryContextExhausted},
		{"400 content filter", 400, "content_policy_violation", CategoryContentFilter},
		{"400 quota exhausted", 400, "arrearage", CategoryQuotaExhausted},
		{"400 invalid request default", 400, "invalid parameter", CategoryInvalidRequest},
		{"429 overload", 429, "server overloaded", CategoryServerOverload},
		{"429 quota", 429, "quota exceeded", CategoryQuotaExhausted},
		{"429 rpm", 429, "requests per minute limit reached", CategoryRateLimitRPM},
		{"429 tpm", 429, "tokens per minute limit reached", CategoryRateLimitTPM},
		{"429 concurrent", 429, "too many concurrent requests", CategoryRateLimitConcurrency},
		{"429 generic", 429, "slow down", CategoryRateLimitGeneric},
		{"498 server overload", 498, "", CategoryServerOverload},
		{"503 server overload", 503, "", CategoryServerOverload},
		{"529 server overload", 529, "", CategoryServerOverload},
		{"504 timeout", 504, "", CategoryTimeout},
		{"500 server error", 500, "", CategoryServerError},
		{"401 auth", 401, "", CategoryAuth},
		{"403 permission", 403, "", CategoryPermission},
		{"404 model not found", 404, "", CategoryModelNotFound},
		{"499 cancelled", 499, "", CategoryCancelled},
		{"408 timeout", 408, "", CategoryTimeout},
		{"500 with context keywords", 500, "context window exceeds limit", CategoryContextExhausted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyErrorCommon(tt.statusCode, tt.body); got != tt.want {
				t.Errorf("classifyErrorCommon(%d, %q) = %v, want %v", tt.statusCode, tt.body, got, tt.want)
			}
		})
	}
}
