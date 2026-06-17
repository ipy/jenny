package api

import (
	"testing"
)

func TestClassifyErrorInternational(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		statusCode int
		body       string
		want       ErrorCategory
	}{
		// OpenRouter
		{
			name:       "OpenRouter context_length_exceeded",
			provider:   "openrouter",
			statusCode: 400,
			body:       `{"error": {"metadata": {"error_type": "context_length_exceeded"}}}`,
			want:       CategoryContextExhausted,
		},
		{
			name:       "OpenRouter max_tokens_exceeded",
			provider:   "OpenRouter",
			statusCode: 400,
			body:       `{"error": {"metadata": {"error_type": "max_tokens_exceeded"}}}`,
			want:       CategoryContextExhausted,
		},
		{
			name:       "OpenRouter content_policy_violation",
			provider:   "openrouter",
			statusCode: 400,
			body:       `{"error": {"metadata": {"error_type": "content_policy_violation"}}}`,
			want:       CategoryContentFilter,
		},
		{
			name:       "OpenRouter rate_limit_exceeded",
			provider:   "openrouter",
			statusCode: 429,
			body:       `{"error": {"metadata": {"error_type": "rate_limit_exceeded"}}}`,
			want:       CategoryRateLimitGeneric,
		},
		{
			name:       "OpenRouter payment_required",
			provider:   "openrouter",
			statusCode: 402,
			body:       `{"error": {"metadata": {"error_type": "payment_required"}}}`,
			want:       CategoryQuotaExhausted,
		},
		{
			name:       "OpenRouter token_limit_exceeded",
			provider:   "openrouter",
			statusCode: 429,
			body:       `{"error": {"metadata": {"error_type": "token_limit_exceeded"}}}`,
			want:       CategoryQuotaExhausted,
		},
		{
			name:       "OpenRouter provider_overloaded",
			provider:   "openrouter",
			statusCode: 503,
			body:       `{"error": {"metadata": {"error_type": "provider_overloaded"}}}`,
			want:       CategoryServerOverload,
		},
		{
			name:       "OpenRouter authentication",
			provider:   "openrouter",
			statusCode: 401,
			body:       `{"error": {"metadata": {"error_type": "authentication"}}}`,
			want:       CategoryAuth,
		},
		{
			name:       "OpenRouter permission_denied",
			provider:   "openrouter",
			statusCode: 403,
			body:       `{"error": {"metadata": {"error_type": "permission_denied"}}}`,
			want:       CategoryPermission,
		},
		{
			name:       "OpenRouter invalid_request",
			provider:   "openrouter",
			statusCode: 400,
			body:       `{"error": {"metadata": {"error_type": "invalid_request"}}}`,
			want:       CategoryInvalidRequest,
		},
		{
			name:       "OpenRouter not_found",
			provider:   "openrouter",
			statusCode: 404,
			body:       `{"error": {"metadata": {"error_type": "not_found"}}}`,
			want:       CategoryModelNotFound,
		},
		{
			name:       "OpenRouter provider_unavailable",
			provider:   "openrouter",
			statusCode: 503,
			body:       `{"error": {"metadata": {"error_type": "provider_unavailable"}}}`,
			want:       CategoryServerError,
		},
		{
			name:       "OpenRouter timeout",
			provider:   "openrouter",
			statusCode: 408,
			body:       `{"error": {"metadata": {"error_type": "timeout"}}}`,
			want:       CategoryTimeout,
		},
		{
			name:       "OpenRouter server",
			provider:   "openrouter",
			statusCode: 500,
			body:       `{"error": {"metadata": {"error_type": "server"}}}`,
			want:       CategoryServerError,
		},
		{
			name:       "OpenRouter top-level type",
			provider:   "openrouter",
			statusCode: 400,
			body:       `{"error": {"type": "invalid_prompt"}}`,
			want:       CategoryInvalidRequest,
		},

		// AWS Bedrock
		{
			name:       "Bedrock IncompleteSignature",
			provider:   "bedrock",
			statusCode: 400,
			body:       `{"__type": "IncompleteSignature"}`,
			want:       CategoryAuth,
		},
		{
			name:       "Bedrock NotAuthorized",
			provider:   "bedrock",
			statusCode: 400,
			body:       `{"__type": "com.amazonaws.bedrock#NotAuthorized"}`,
			want:       CategoryPermission,
		},
		{
			name:       "Bedrock AccessDeniedException",
			provider:   "aws-bedrock",
			statusCode: 403,
			body:       `{"__type": "AccessDeniedException"}`,
			want:       CategoryPermission,
		},
		{
			name:       "Bedrock InvalidClientTokenId",
			provider:   "bedrock",
			statusCode: 403,
			body:       `{"__type": "InvalidClientTokenId"}`,
			want:       CategoryAuth,
		},
		{
			name:       "Bedrock ThrottlingException",
			provider:   "bedrock",
			statusCode: 429,
			body:       `{"__type": "ThrottlingException"}`,
			want:       CategoryRateLimitGeneric,
		},
		{
			name:       "Bedrock ServiceUnavailable",
			provider:   "bedrock",
			statusCode: 503,
			body:       `{"__type": "ServiceUnavailable"}`,
			want:       CategoryServerOverload,
		},
		{
			name:       "Bedrock ValidationError (fallback)",
			provider:   "bedrock",
			statusCode: 400,
			body:       `{"__type": "ValidationError"}`,
			want:       CategoryUnknown,
		},

		// Fireworks
		{
			name:       "Fireworks 429 capacity",
			provider:   "fireworks",
			statusCode: 429,
			body:       `{"message": "Account has insufficient capacity"}`,
			want:       CategoryServerOverload,
		},
		{
			name:       "Fireworks 429 dedicated",
			provider:   "fireworks",
			statusCode: 429,
			body:       `{"message": "dedicated capacity reached"}`,
			want:       CategoryServerOverload,
		},
		{
			name:       "Fireworks 429 generic",
			provider:   "fireworks",
			statusCode: 429,
			body:       `{"message": "Rate limit exceeded"}`,
			want:       CategoryRateLimitGeneric,
		},
		{
			name:       "Fireworks 413",
			provider:   "fireworks",
			statusCode: 413,
			body:       `{}`,
			want:       CategoryContextExhausted,
		},
		{
			name:       "Fireworks 412 suspend",
			provider:   "fireworks",
			statusCode: 412,
			body:       `{"message": "Account suspended"}`,
			want:       CategoryQuotaExhausted,
		},
		{
			name:       "Fireworks 412 account",
			provider:   "fireworks",
			statusCode: 412,
			body:       `{"message": "Check your account status"}`,
			want:       CategoryQuotaExhausted,
		},
		{
			name:       "Fireworks 412 other",
			provider:   "fireworks",
			statusCode: 412,
			body:       `{"message": "precondition failed"}`,
			want:       CategoryInvalidRequest,
		},
		{
			name:       "Fireworks 500",
			provider:   "fireworks",
			statusCode: 500,
			body:       `{}`,
			want:       CategoryServerError,
		},

		// Groq
		{
			name:       "Groq 498",
			provider:   "groq",
			statusCode: 498,
			body:       `{}`,
			want:       CategoryServerOverload,
		},
		{
			name:       "Groq 413",
			provider:   "groq",
			statusCode: 413,
			body:       `{}`,
			want:       CategoryContextExhausted,
		},
		{
			name:       "Groq 499",
			provider:   "groq",
			statusCode: 499,
			body:       `{}`,
			want:       CategoryCancelled,
		},
		{
			name:       "Groq invalid_request_error",
			provider:   "groq",
			statusCode: 400,
			body:       `{"error": {"type": "invalid_request_error"}}`,
			want:       CategoryInvalidRequest,
		},

		// Unknown fallback
		{
			name:       "Unknown provider",
			provider:   "unknown",
			statusCode: 400,
			body:       `{}`,
			want:       CategoryUnknown,
		},
		{
			name:       "OpenRouter unknown type",
			provider:   "openrouter",
			statusCode: 400,
			body:       `{"error": {"metadata": {"error_type": "weird_error"}}}`,
			want:       CategoryUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyErrorInternational(tt.provider, tt.statusCode, tt.body)
			if got != tt.want {
				t.Errorf("classifyErrorInternational() = %v, want %v", got, tt.want)
			}
		})
	}
}
