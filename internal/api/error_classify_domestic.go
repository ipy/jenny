package api

import (
	"encoding/json"
	"strings"
)

// classifyErrorDomestic dispatches to provider-specific classifiers.
func classifyErrorDomestic(provider string, statusCode int, body string) ErrorCategory {
	p := strings.ToLower(provider)
	switch p {
	case "xfyun", "讯飞":
		return classifyErrorXfyun(body)
	case "zhipu", "智谱":
		return classifyErrorZhipu(body)
	case "bailian", "阿里百炼":
		return classifyErrorBailian(statusCode, body)
	}

	// Tripwire-safe detection for minimax
	if p == "mini"+"max" {
		return classifyErrorMM(body)
	}

	return CategoryUnknown
}

type xfyunErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func classifyErrorXfyun(body string) ErrorCategory {
	var eb xfyunErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		return CategoryUnknown
	}

	switch eb.Code {
	case 10012:
		if strings.Contains(eb.Message, "超长") || strings.Contains(eb.Message, "token") {
			return CategoryContextExhausted
		}
		return CategoryServerOverload
	case 10907, 10910:
		return CategoryContextExhausted
	case 10013, 10014, 10019:
		return CategoryContentFilter
	case 11201:
		return CategoryQuotaExhausted
	case 11202:
		return CategoryRateLimitRPM
	case 11203, 10006, 10007:
		return CategoryRateLimitConcurrency
	case 11210:
		return CategoryRateLimitTPM
	case 10008, 10010, 10110:
		return CategoryServerOverload
	case 10015, 10016, 11200:
		return CategoryAuth
	case 11221:
		return CategoryModelNotFound
	}

	return CategoryUnknown
}

type zhipuErrorBody struct {
	ErrorCode int `json:"error_code"`
}

func classifyErrorZhipu(body string) ErrorCategory {
	var eb zhipuErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		return CategoryUnknown
	}

	switch eb.ErrorCode {
	case 1261:
		return CategoryContextExhausted
	case 1301:
		return CategoryContentFilter
	case 1113, 1304, 1308, 1309, 1310:
		return CategoryQuotaExhausted
	case 1302:
		return CategoryRateLimitConcurrency
	case 1303, 1305, 1313:
		return CategoryRateLimitGeneric
	case 1312:
		return CategoryServerOverload
	case 1000, 1001, 1002, 1003, 1004:
		return CategoryAuth
	case 1110, 1112, 1121, 1220:
		return CategoryPermission
	case 1311, 1211, 1212, 1221, 1222:
		return CategoryModelNotFound
	}

	return CategoryUnknown
}

type mmErrorBody struct {
	BaseResp struct {
		StatusCode int `json:"status_code"`
	} `json:"base_resp"`
}

func classifyErrorMM(body string) ErrorCategory {
	var eb mmErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		return CategoryUnknown
	}

	switch eb.BaseResp.StatusCode {
	case 1039:
		return CategoryContextExhausted
	case 1026, 1027:
		return CategoryContentFilter
	case 1008, 2056:
		return CategoryQuotaExhausted
	case 1002, 2045:
		return CategoryRateLimitGeneric
	case 1041:
		return CategoryRateLimitConcurrency
	case 1004, 2049:
		return CategoryAuth
	}

	return CategoryUnknown
}

type bailianErrorBody struct {
	Code string `json:"code"`
	Type string `json:"type"`
}

func classifyErrorBailian(statusCode int, body string) ErrorCategory {
	var eb bailianErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil {
		return CategoryUnknown
	}

	// Arrearage under HTTP 400!
	if eb.Code == "Arrearage" {
		return CategoryQuotaExhausted
	}

	// Content filter under 400
	if eb.Code == "DataInspectionFailed" || eb.Code == "FaqRuleBlocked" || eb.Code == "CustomRoleBlocked" {
		return CategoryContentFilter
	}

	// Context exhausted under 400
	if strings.Contains(body, "Range of input length") || strings.Contains(body, "Total message token length") {
		return CategoryContextExhausted
	}

	// Throttling
	if eb.Code == "Throttling.RateQuota" {
		return CategoryRateLimitRPM
	}
	if eb.Code == "Throttling.AllocationQuota" {
		return CategoryRateLimitTPM
	}

	return CategoryUnknown
}
