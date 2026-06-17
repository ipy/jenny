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
		// 1. 413 -> CategoryContextExhausted
		{"413 context exhausted", 413, "Request entity too large", CategoryContextExhausted},

		// 2. 402 -> CategoryPaymentRequired
		{"402 payment required", 402, "Payment Required", CategoryPaymentRequired},

		// 3. 400 with context keywords
		{"400 prompt_too_long", 400, `{"error": {"type": "prompt_too_long"}}`, CategoryContextExhausted},
		{"400 context window exceeds limit", 400, "context window exceeds limit", CategoryContextExhausted},
		{"400 token数量超过上限", 400, "token数量超过上限", CategoryContextExhausted},

		// 4. Content filter keywords
		{"400 content_filter", 400, "content_filter", CategoryContentFilter},
		{"400 safety", 400, "safety", CategoryContentFilter},
		{"400 敏感内容", 400, "敏感内容", CategoryContentFilter},

		// 5. Quota / Payment Keywords
		{"400 quota", 400, "quota exceeded", CategoryQuotaExhausted},
		{"400 Arrearage", 400, "Arrearage", CategoryQuotaExhausted},
		{"400 余额不足", 400, "余额不足", CategoryQuotaExhausted},

		// 6. 429 disaggregation
		{"429 overload", 429, "overload", CategoryServerOverload},
		{"429 capacity", 429, "capacity", CategoryServerOverload},
		{"429 quota", 429, "quota", CategoryQuotaExhausted},
		{"429 rpm", 429, "rpm", CategoryRateLimitRPM},
		{"429 requests per minute", 429, "requests per minute", CategoryRateLimitRPM},
		{"429 tpm", 429, "tpm", CategoryRateLimitTPM},
		{"429 tokens per minute", 429, "tokens per minute", CategoryRateLimitTPM},
		{"429 concurrency", 429, "concurrent", CategoryRateLimitConcurrency},
		{"429 generic", 429, "generic rate limit", CategoryRateLimitRPM}, // "rate" matches RPM
		{"429 insufficient", 429, "insufficient credits", CategoryQuotaExhausted},
		{"429 limit exceeded", 429, "quota limit exceeded", CategoryQuotaExhausted},

		// 7. 5xx mapping
		{"529 overloaded", 529, "", CategoryServerOverload},
		{"503 service unavailable", 503, "", CategoryServerOverload},
		{"498 overloaded", 498, "", CategoryServerOverload},
		{"504 timeout", 504, "", CategoryTimeout},
		{"500 server error", 500, "", CategoryServerError},
		{"500 context_length_exceeded", 500, "context_length_exceeded", CategoryContextExhausted},

		// 8. Specific 4xx
		{"401 auth", 401, "", CategoryAuth},
		{"401 with safety keyword", 401, "safety", CategoryAuth}, // Verify gate
		{"403 permission", 403, "", CategoryPermission},
		{"499 cancelled", 499, "", CategoryCancelled},
		{"404 model not found", 404, "", CategoryModelNotFound},


		// 9. 400 default
		{"400 invalid request", 400, "invalid parameter", CategoryInvalidRequest},

		// Unknown
		{"200 unknown", 200, "", CategoryUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyErrorCommon(tt.statusCode, tt.body); got != tt.want {
				t.Errorf("classifyErrorCommon(%d, %q) = %v, want %v", tt.statusCode, tt.body, got, tt.want)
			}
		})
	}
}
