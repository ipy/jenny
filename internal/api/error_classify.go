package api

import (
	"strings"
)

// classifyErrorCommon provides universal keyword-based error classification.
func classifyErrorCommon(statusCode int, body string) ErrorCategory {
	lowerBody := strings.ToLower(body)

	// 1. 413 -> CategoryContextExhausted (Claude/Fireworks/OpenRouter/Groq)
	if statusCode == 413 {
		return CategoryContextExhausted
	}

	// 2. 402 -> CategoryPaymentRequired
	if statusCode == 402 {
		return CategoryPaymentRequired
	}

	// 3. Keyword scan for specific semantics (highest priority for 400/500/504)
	if statusCode == 400 || statusCode == 500 || statusCode == 504 || statusCode == 422 {
		// Context Keywords
		contextKeywords := []string{
			"context_length_exceeded",
			"prompt_too_long",
			"context window exceeds limit",
			"maximum context length",
			"too many tokens",
			"size limit exceeded",
			"token limit exceeded",
			"input token length too long",
			"context length exceeded",
			"prompt length exceeded",
			"chat context length exceeded",
			"input data length exceeded",
			"payload_too_large",
			"request_too_large",
			"exceed model token limit",
			"token数量超过上限",
			"上下文超长",
			"上下文超限",
			"prompt超长",
			"range of input length",
			"total message token length",
		}
		for _, kw := range contextKeywords {
			if strings.Contains(lowerBody, kw) {
				return CategoryContextExhausted
			}
		}

		// Content Filter Keywords
		filterKeywords := []string{
			"content_policy_violation",
			"content_filter",
			"safety",
			"refusal",
			"inappropriate",
			"offensive",
			"敏感内容",
			"datainspectionfailed",
			"faqruleblocked",
			"customroleblocked",
		}
		for _, kw := range filterKeywords {
			if strings.Contains(lowerBody, kw) {
				return CategoryContentFilter
			}
		}

		// Quota / Payment Keywords
		quotaKeywords := []string{
			"arrearage",
			"quota",
			"payment_required",
			"insufficient_quota",
			"billing",
			"次数超限",
			"余额不足",
			"exceed quota",
		}
		for _, kw := range quotaKeywords {
			if strings.Contains(lowerBody, kw) {
				return CategoryQuotaExhausted
			}
		}
	}

	// 4. 429 Disaggregation
	if statusCode == 429 {
		// ServerOverload
		if strings.Contains(lowerBody, "overload") ||
			strings.Contains(lowerBody, "繁忙") ||
			strings.Contains(lowerBody, "排队") ||
			strings.Contains(lowerBody, "capacity") ||
			strings.Contains(lowerBody, "busy") ||
			strings.Contains(lowerBody, "heavy load") ||
			strings.Contains(lowerBody, "provider_overloaded") {
			return CategoryServerOverload
		}
		// QuotaExhausted
		if strings.Contains(lowerBody, "quota") ||
			strings.Contains(lowerBody, "insufficient") ||
			strings.Contains(lowerBody, "余额") ||
			strings.Contains(lowerBody, "次数") ||
			strings.Contains(lowerBody, "limit exceeded") {
			return CategoryQuotaExhausted
		}
		// RateLimitRPM
		if strings.Contains(lowerBody, "rate") ||
			strings.Contains(lowerBody, "rpm") ||
			strings.Contains(lowerBody, "requests per") ||
			strings.Contains(lowerBody, "秒级流控") {
			return CategoryRateLimitRPM
		}
		// RateLimitTPM
		if strings.Contains(lowerBody, "token per") ||
			strings.Contains(lowerBody, "tpm") ||
			strings.Contains(lowerBody, "tokens per minute") {
			return CategoryRateLimitTPM
		}
		// RateLimitConcurrency
		if strings.Contains(lowerBody, "concurrent") ||
			strings.Contains(lowerBody, "并发") ||
			strings.Contains(lowerBody, "simultaneous") {
			return CategoryRateLimitConcurrency
		}
		return CategoryRateLimitGeneric
	}

	// 5. 5xx mapping
	switch statusCode {
	case 529, 503, 498:
		return CategoryServerOverload
	case 504:
		return CategoryTimeout
	case 500:
		return CategoryServerError
	}

	// 6. Specific codes
	switch statusCode {
	case 401:
		return CategoryAuth
	case 403:
		return CategoryPermission
	case 499:
		return CategoryCancelled
	case 404:
		return CategoryModelNotFound
	case 408:
		return CategoryTimeout
	}

	// 7. 400 default
	if statusCode == 400 {
		return CategoryInvalidRequest
	}

	return CategoryUnknown
}
