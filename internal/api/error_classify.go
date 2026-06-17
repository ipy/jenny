package api

import (
	"strings"
)

// classifyErrorCommon classifies HTTP status codes and response bodies into ErrorCategory.
func classifyErrorCommon(statusCode int, body string) ErrorCategory {
	bodyLower := strings.ToLower(body)

	// 1. 413 -> CategoryContextExhausted
	if statusCode == 413 {
		return CategoryContextExhausted
	}

	// 2. 402 -> CategoryPaymentRequired
	if statusCode == 402 {
		return CategoryPaymentRequired
	}

	// 3. 429 disaggregation (high priority)
	if statusCode == 429 {
		overloadKeywords := []string{"overload", "繁忙", "排队", "capacity", "busy", "heavy load", "provider_overloaded"}
		for _, kw := range overloadKeywords {
			if strings.Contains(bodyLower, kw) {
				return CategoryServerOverload
			}
		}

		// Quota inside 429
		quotaKeywords429 := []string{"quota", "insufficient", "余额", "次数", "limit exceeded"}
		for _, kw := range quotaKeywords429 {
			if strings.Contains(bodyLower, strings.ToLower(kw)) {
				return CategoryQuotaExhausted
			}
		}

		if strings.Contains(bodyLower, "rpm") || strings.Contains(bodyLower, "requests per") || strings.Contains(bodyLower, "秒级流控") || strings.Contains(bodyLower, "rate") {
			return CategoryRateLimitRPM
		}
		if strings.Contains(bodyLower, "tpm") || strings.Contains(bodyLower, "tokens per") || strings.Contains(bodyLower, "token per") {
			return CategoryRateLimitTPM
		}
		if strings.Contains(bodyLower, "concurrent") || strings.Contains(bodyLower, "并发") || strings.Contains(bodyLower, "simultaneous") {
			return CategoryRateLimitConcurrency
		}
		return CategoryRateLimitGeneric
	}

	// 4. Keyword scan — ONLY for 400/500/504
	if statusCode == 400 || statusCode == 500 || statusCode == 504 {
		// Context keywords first
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
			if strings.Contains(bodyLower, strings.ToLower(kw)) {
				return CategoryContextExhausted
			}
		}

		// Content filter keywords
		filterKeywords := []string{
			"content_policy_violation",
			"content_filter",
			"safety",
			"refusal",
			"inappropriate",
			"offensive",
			"敏感内容",
			"DataInspectionFailed",
			"FaqRuleBlocked",
			"CustomRoleBlocked",
		}
		for _, kw := range filterKeywords {
			if strings.Contains(bodyLower, strings.ToLower(kw)) {
				return CategoryContentFilter
			}
		}

		// Quota / Payment Keywords
		quotaKeywords := []string{
			"Arrearage",
			"quota",
			"payment_required",
			"insufficient_quota",
			"billing",
			"次数超限",
			"余额不足",
			"exceed quota",
			"insufficient",
			"limit exceeded",
		}
		for _, kw := range quotaKeywords {
			if strings.Contains(bodyLower, strings.ToLower(kw)) {
				return CategoryQuotaExhausted
			}
		}
	}

	// 5. 5xx mapping
	switch statusCode {
	case 529, 503, 498:
		return CategoryServerOverload
	case 504:
		return CategoryTimeout
	}
	if statusCode >= 500 {
		return CategoryServerError
	}

	// 6. Specific 4xx
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
