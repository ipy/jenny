package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// ConfigHooks holds hooks loaded from settings.json.
type ConfigHooks struct {
	PreToolUse  []HookEntry `json:"PreToolUse,omitempty"`
	PostToolUse []HookEntry `json:"PostToolUse,omitempty"`
}

// HookEntry represents a hook matcher and its associated commands.
type HookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookCommand represents a single hook command.
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

const defaultHookTimeout = 60

// RunPreToolUse executes PreToolUse hooks that match the given tool.
// Returns (blocked, reason) — blocked is true if any hook exited non-zero.
func (h *ConfigHooks) RunPreToolUse(ctx context.Context, toolName string, input map[string]any) (bool, string) {
	if h == nil || len(h.PreToolUse) == 0 {
		return false, ""
	}

	for _, entry := range h.PreToolUse {
		if !matchesHookEntry(entry.Matcher, toolName) {
			continue
		}
		for _, hook := range entry.Hooks {
			if hook.Type != "command" || hook.Command == "" {
				continue
			}
			timeout := hook.Timeout
			if timeout <= 0 {
				timeout = defaultHookTimeout
			}
			hookCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)

			cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
			cmd.Env = buildHookEnv(toolName, input)
			output, err := cmd.CombinedOutput()
			cancel()

			if err != nil {
				reason := fmt.Sprintf("PreToolUse hook blocked %s: %s", toolName, strings.TrimSpace(string(output)))
				log.Debug("config hook blocked tool", "tool", toolName, "hook", hook.Command, "err", err)
				return true, reason
			}
		}
	}
	return false, ""
}

// RunPostToolUse executes PostToolUse hooks that match the given tool.
func (h *ConfigHooks) RunPostToolUse(ctx context.Context, toolName string, input map[string]any, output string) {
	if h == nil || len(h.PostToolUse) == 0 {
		return
	}

	for _, entry := range h.PostToolUse {
		if !matchesHookEntry(entry.Matcher, toolName) {
			continue
		}
		for _, hook := range entry.Hooks {
			if hook.Type != "command" || hook.Command == "" {
				continue
			}
			timeout := hook.Timeout
			if timeout <= 0 {
				timeout = defaultHookTimeout
			}
			hookCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)

			cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
			env := buildHookEnv(toolName, input)
			truncated := output
			if len(truncated) > 10240 {
				truncated = truncated[:10240]
			}
			env = append(env, "TOOL_OUTPUT="+truncated)
			cmd.Env = env
			_, err := cmd.CombinedOutput()
			cancel()

			if err != nil {
				log.Debug("PostToolUse hook error", "tool", toolName, "hook", hook.Command, "err", err)
			}
		}
	}
}

func matchesHookEntry(matcher, toolName string) bool {
	if matcher == "" {
		return true
	}
	return matcher == toolName
}

func buildHookEnv(toolName string, input map[string]any) []string {
	env := []string{
		"TOOL_NAME=" + toolName,
	}
	if input != nil {
		if data, err := json.Marshal(input); err == nil {
			env = append(env, "TOOL_INPUT="+string(data))
		}
	}
	return env
}

// ParseConfigHooks parses the hooks section from a raw JSON value.
func ParseConfigHooks(raw map[string]any) *ConfigHooks {
	hooksRaw, ok := raw["hooks"]
	if !ok {
		return nil
	}

	data, err := json.Marshal(hooksRaw)
	if err != nil {
		return nil
	}

	var hooks ConfigHooks
	if err := json.Unmarshal(data, &hooks); err != nil {
		return nil
	}

	if len(hooks.PreToolUse) == 0 && len(hooks.PostToolUse) == 0 {
		return nil
	}
	return &hooks
}
