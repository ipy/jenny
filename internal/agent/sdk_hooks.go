package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/ipy/jenny/internal/log"
)

// SDKHookRunner is a function that runs SDK PreToolUse hooks for a given tool.
// Returns (blocked bool, reason string). If blocked, the tool should not execute.
// Set on StreamConfig.SDKHookRunner when SDK input mode is active.
type SDKHookRunner func(ctx context.Context, toolName string, toolInput any, toolUseID string, sessionID string, cwd string) (blocked bool, reason string)

// RunPreToolUseHooks fires the SDK hook runner if configured.
func RunPreToolUseHooks(ctx context.Context, cfg *StreamConfig, toolName string, toolInput any, toolUseID string, sessionID string, cwd string) (bool, string) {
	if cfg == nil || cfg.SDKHookRunner == nil {
		return false, ""
	}
	return cfg.SDKHookRunner(ctx, toolName, toolInput, toolUseID, sessionID, cwd)
}

// BuildSDKHookRunner creates an SDKHookRunner from SDK hook registration data.
// This is called from main.go after the SDK initialize handshake.
type SDKHookConfig struct {
	Matchers     map[string][]SDKHookMatcher
	SendCallback func(ctx context.Context, callbackID string, input map[string]any) (*SDKHookCallbackResponse, error)
}

// SDKHookMatcher describes a registered SDK hook matcher.
type SDKHookMatcher struct {
	Matcher         string
	HookCallbackIDs []string
	Timeout         int
}

// SDKHookCallbackResponse is the response from an SDK hook callback.
type SDKHookCallbackResponse struct {
	Continue           *bool
	Decision           string
	StopReason         string
	SuppressOutput     bool
	HookSpecificOutput json.RawMessage
}

// SDKPreToolUseOutput is the hookSpecificOutput for PreToolUse hooks.
type SDKPreToolUseOutput struct {
	HookEventName      string          `json:"hookEventName"`
	PermissionDecision string          `json:"permissionDecision,omitempty"`
	UpdatedInput       json.RawMessage `json:"updatedInput,omitempty"`
}

// NewSDKHookRunner creates an SDKHookRunner from the given config.
func NewSDKHookRunner(cfg *SDKHookConfig) SDKHookRunner {
	return func(ctx context.Context, toolName string, toolInput any, toolUseID string, sessionID string, cwd string) (bool, string) {
		matchers, ok := cfg.Matchers["PreToolUse"]
		if !ok || len(matchers) == 0 {
			return false, ""
		}

		for _, m := range matchers {
			if !matchToolName(m.Matcher, toolName) {
				continue
			}

			for _, callbackID := range m.HookCallbackIDs {
				hookInput := map[string]any{
					"hook_event_name": "PreToolUse",
					"tool_name":       toolName,
					"tool_input":      toolInput,
					"tool_use_id":     toolUseID,
					"session_id":      sessionID,
					"cwd":             cwd,
				}

				resp, err := cfg.SendCallback(ctx, callbackID, hookInput)
				if err != nil {
					log.Warn("SDK hook callback failed", "callback_id", callbackID, "error", err)
					continue
				}

				if resp.Decision == "block" {
					reason := resp.StopReason
					if reason == "" {
						reason = "blocked by SDK hook"
					}
					return true, reason
				}

				if resp.Continue != nil && !*resp.Continue {
					reason := resp.StopReason
					if reason == "" {
						reason = "stopped by SDK hook"
					}
					return true, reason
				}

				if len(resp.HookSpecificOutput) > 0 {
					var preToolOut SDKPreToolUseOutput
					if err := json.Unmarshal(resp.HookSpecificOutput, &preToolOut); err == nil {
						if preToolOut.PermissionDecision == "deny" {
							return true, "denied by SDK hook"
						}
					}
				}
			}
		}

		return false, ""
	}
}

// matchToolName checks if a tool name matches a pipe-separated pattern.
func matchToolName(pattern, toolName string) bool {
	if pattern == "" {
		return true
	}
	parts := strings.Split(pattern, "|")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == toolName {
			return true
		}
		if matched, _ := regexp.MatchString("^"+p+"$", toolName); matched {
			return true
		}
	}
	return false
}
