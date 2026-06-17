package api

import (
	"fmt"
	"testing"
)

func TestClassifyErrorDomestic(t *testing.T) {
	tests := []struct {
		provider   string
		statusCode int
		body       string
		expected   ErrorCategory
	}{
		// Xfyun
		{"xfyun", 200, `{"code": 10012, "message": "token超长"}`, CategoryContextExhausted},
		{"讯飞", 200, `{"code": 10012, "message": "服务器忙"}`, CategoryServerOverload},
		{"xfyun", 200, `{"code": 10907}`, CategoryContextExhausted},
		{"xfyun", 200, `{"code": 10013}`, CategoryContentFilter},
		{"xfyun", 200, `{"code": 11201}`, CategoryQuotaExhausted},
		{"xfyun", 200, `{"code": 11202}`, CategoryRateLimitRPM},
		{"xfyun", 200, `{"code": 11203}`, CategoryRateLimitConcurrency},
		{"xfyun", 200, `{"code": 11210}`, CategoryRateLimitTPM},
		{"xfyun", 200, `{"code": 10008}`, CategoryServerOverload},
		{"xfyun", 200, `{"code": 10015}`, CategoryAuth},
		{"xfyun", 200, `{"code": 11221}`, CategoryModelNotFound},
		{"xfyun", 200, `{"code": 99999}`, CategoryUnknown},

		// Zhipu
		{"zhipu", 200, `{"error_code": 1261}`, CategoryContextExhausted},
		{"智谱", 200, `{"error_code": 1301}`, CategoryContentFilter},
		{"zhipu", 200, `{"error_code": 1113}`, CategoryQuotaExhausted},
		{"zhipu", 200, `{"error_code": 1302}`, CategoryRateLimitConcurrency},
		{"zhipu", 200, `{"error_code": 1303}`, CategoryRateLimitGeneric},
		{"zhipu", 200, `{"error_code": 1312}`, CategoryServerOverload},
		{"zhipu", 200, `{"error_code": 1000}`, CategoryAuth},
		{"zhipu", 200, `{"error_code": 1110}`, CategoryPermission},
		{"zhipu", 200, `{"error_code": 1311}`, CategoryModelNotFound},
		{"zhipu", 200, `{"error_code": 9999}`, CategoryUnknown},

		// MiniMax
		{"mini" + "max", 200, `{"base_resp": {"status_code": 1039}}`, CategoryContextExhausted},
		{"mini" + "max", 200, `{"base_resp": {"status_code": 1026}}`, CategoryContentFilter},
		{"mini" + "max", 200, `{"base_resp": {"status_code": 1008}}`, CategoryQuotaExhausted},
		{"mini" + "max", 200, `{"base_resp": {"status_code": 1002}}`, CategoryRateLimitGeneric},
		{"mini" + "max", 200, `{"base_resp": {"status_code": 1041}}`, CategoryRateLimitConcurrency},
		{"mini" + "max", 200, `{"base_resp": {"status_code": 1004}}`, CategoryAuth},
		{"mini" + "max", 200, `{"base_resp": {"status_code": 9999}}`, CategoryUnknown},

		// Bailian
		{"bailian", 400, `{"code": "Arrearage"}`, CategoryQuotaExhausted},
		{"阿里百炼", 400, `{"code": "DataInspectionFailed"}`, CategoryContentFilter},
		{"bailian", 400, `{"code": "FaqRuleBlocked"}`, CategoryContentFilter},
		{"bailian", 400, `{"code": "CustomRoleBlocked"}`, CategoryContentFilter},
		{"bailian", 400, `{"message": "Range of input length"}`, CategoryContextExhausted},
		{"bailian", 400, `{"message": "Total message token length"}`, CategoryContextExhausted},
		{"bailian", 200, `{"code": "Throttling.RateQuota"}`, CategoryRateLimitRPM},
		{"bailian", 200, `{"code": "Throttling.AllocationQuota"}`, CategoryRateLimitTPM},
		{"bailian", 400, `{"code": "UnknownCode"}`, CategoryUnknown},

		// Unknown provider
		{"unknown", 200, `{}`, CategoryUnknown},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("Test_%d_%s", i, tt.provider), func(t *testing.T) {
			got := classifyErrorDomestic(tt.provider, tt.statusCode, tt.body)
			if got != tt.expected {
				t.Errorf("classifyErrorDomestic(%q, %d, %q) = %v; want %v", tt.provider, tt.statusCode, tt.body, got, tt.expected)
			}
		})
	}
}

func TestClassifyErrorXfyunDualSemantics(t *testing.T) {
	tests := []struct {
		message  string
		expected ErrorCategory
	}{
		{"prompt超长", CategoryContextExhausted},
		{"token limit exceeded", CategoryContextExhausted},
		{"服务器忙", CategoryServerOverload},
		{"", CategoryServerOverload},
	}

	for _, tt := range tests {
		body := fmt.Sprintf(`{"code": 10012, "message": %q}`, tt.message)
		got := classifyErrorXfyun(body)
		if got != tt.expected {
			t.Errorf("classifyErrorXfyun(%q) = %v; want %v", body, got, tt.expected)
		}
	}
}
