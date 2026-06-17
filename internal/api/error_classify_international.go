package api

import (
	"encoding/json"
	"strings"
)

// classifyErrorInternational dispatches to international provider-specific classifiers.
func classifyErrorInternational(provider string, statusCode int, body string) ErrorCategory {
	p := strings.ToLower(provider)
	switch p {
	case "openrouter":
		return classifyErrorOpenRouter(body)
	case "bedrock", "aws-bedrock":
		return classifyErrorBedrock(statusCode, body)
	case "fireworks":
		return classifyErrorFireworks(statusCode, body)
	case "groq":
		return classifyErrorGroq(statusCode, body)
	}
	return CategoryUnknown
}

type openRouterErrorBody struct {
	Error struct {
		Type     string `json:"type"`
		Message  string `json:"message"`
		Metadata struct {
			ErrorType string `json:"error_type"`
		} `json:"metadata"`
	} `json:"error"`
}

func classifyErrorOpenRouter(body string) ErrorCategory {
	var eb openRouterErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		return CategoryUnknown
	}

	errorType := eb.Error.Metadata.ErrorType
	if errorType == "" {
		errorType = eb.Error.Type
	}

	switch errorType {
	case "context_length_exceeded", "max_tokens_exceeded", "payload_too_large":
		return CategoryContextExhausted
	case "content_policy_violation", "refusal":
		return CategoryContentFilter
	case "rate_limit_exceeded":
		return CategoryRateLimitGeneric
	case "payment_required", "token_limit_exceeded":
		return CategoryQuotaExhausted
	case "provider_overloaded":
		return CategoryServerOverload
	case "authentication":
		return CategoryAuth
	case "permission_denied":
		return CategoryPermission
	case "invalid_request", "invalid_prompt", "string_too_long", "unprocessable":
		return CategoryInvalidRequest
	case "not_found":
		return CategoryModelNotFound
	case "provider_unavailable":
		return CategoryServerError
	case "timeout":
		return CategoryTimeout
	case "server":
		return CategoryServerError
	}

	return CategoryUnknown
}

type bedrockErrorBody struct {
	Type_   string `json:"__type"`
	Message string `json:"message"`
}

func classifyErrorBedrock(statusCode int, body string) ErrorCategory {
	var eb bedrockErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		return CategoryUnknown
	}

	// Remove prefix if present (e.g. "com.amazonaws.bedrock#")
	typ := eb.Type_
	if idx := strings.LastIndex(typ, "#"); idx != -1 {
		typ = typ[idx+1:]
	}

	switch typ {
	case "IncompleteSignature":
		if statusCode == 400 {
			return CategoryAuth
		}
	case "NotAuthorized":
		if statusCode == 400 {
			return CategoryPermission
		}
	case "AccessDeniedException":
		if statusCode == 403 {
			return CategoryPermission
		}
	case "InvalidClientTokenId":
		if statusCode == 403 {
			return CategoryAuth
		}
	case "ThrottlingException":
		return CategoryRateLimitGeneric
	case "ServiceUnavailable":
		return CategoryServerOverload
	case "ValidationError":
		// Fall through to common keyword scan to catch context-limit messages
		return CategoryUnknown
	}

	return CategoryUnknown
}

func classifyErrorFireworks(statusCode int, body string) ErrorCategory {
	bodyLower := strings.ToLower(body)
	switch statusCode {
	case 429:
		if strings.Contains(bodyLower, "dedicated") || strings.Contains(bodyLower, "capacity") {
			return CategoryServerOverload
		}
		return CategoryRateLimitGeneric
	case 413:
		return CategoryContextExhausted
	case 412:
		if strings.Contains(bodyLower, "suspend") || strings.Contains(bodyLower, "account") {
			return CategoryQuotaExhausted
		}
		return CategoryInvalidRequest
	case 500:
		return CategoryServerError
	}
	return CategoryUnknown
}

type groqErrorBody struct {
	Error struct {
		Type string `json:"type"`
	} `json:"error"`
}

func classifyErrorGroq(statusCode int, body string) ErrorCategory {
	switch statusCode {
	case 498:
		return CategoryServerOverload
	case 413:
		return CategoryContextExhausted
	case 499:
		return CategoryCancelled
	}

	var eb groqErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err == nil {
		if eb.Error.Type == "invalid_request_error" {
			return CategoryInvalidRequest
		}
	}

	return CategoryUnknown
}
