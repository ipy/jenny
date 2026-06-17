package api

// ErrorCategory represents a normalized category for LLM API errors.
type ErrorCategory string

const (
	CategoryUnknown               ErrorCategory = "unknown"
	CategoryAuth                  ErrorCategory = "auth"
	CategoryPermission            ErrorCategory = "permission"
	CategoryInvalidRequest        ErrorCategory = "invalid_request"
	CategoryContextExhausted      ErrorCategory = "context_exhausted"
	CategoryRateLimitRPM          ErrorCategory = "rate_limit_rpm"
	CategoryRateLimitTPM          ErrorCategory = "rate_limit_tpm"
	CategoryRateLimitConcurrency  ErrorCategory = "rate_limit_concurrency"
	CategoryRateLimitGeneric      ErrorCategory = "rate_limit_generic"
	CategoryQuotaExhausted        ErrorCategory = "quota_exhausted"
	CategoryPaymentRequired       ErrorCategory = "payment_required"
	CategoryContentFilter         ErrorCategory = "content_filter"
	CategoryServerOverload        ErrorCategory = "server_overload"
	CategoryServerError           ErrorCategory = "server_error"
	CategoryTimeout               ErrorCategory = "timeout"
	CategoryCancelled             ErrorCategory = "cancelled"
	CategoryModelNotFound         ErrorCategory = "model_not_found"
	CategoryOutputCapHit          ErrorCategory = "output_cap_hit"
)

func (c ErrorCategory) String() string {
	return string(c)
}
