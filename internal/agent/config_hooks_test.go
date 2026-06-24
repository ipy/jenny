package agent

import (
	"context"
	"testing"
)

func TestConfigHooksPreToolUse(t *testing.T) {
	hooks := &ConfigHooks{
		PreToolUse: []HookEntry{
			{
				Matcher: "Bash",
				Hooks: []HookCommand{
					{Type: "command", Command: "exit 0", Timeout: 5},
				},
			},
		},
	}

	blocked, _ := hooks.RunPreToolUse(context.Background(), "Bash", map[string]any{"command": "ls"})
	if blocked {
		t.Error("hook with exit 0 should not block")
	}

	blocked, _ = hooks.RunPreToolUse(context.Background(), "Read", map[string]any{"file_path": "foo"})
	if blocked {
		t.Error("non-matching tool should not be blocked")
	}
}

func TestConfigHooksPreToolUseBlocking(t *testing.T) {
	hooks := &ConfigHooks{
		PreToolUse: []HookEntry{
			{
				Matcher: "Bash",
				Hooks: []HookCommand{
					{Type: "command", Command: "exit 1", Timeout: 5},
				},
			},
		},
	}

	blocked, reason := hooks.RunPreToolUse(context.Background(), "Bash", map[string]any{"command": "rm -rf /"})
	if !blocked {
		t.Error("hook with exit 1 should block")
	}
	if reason == "" {
		t.Error("blocked hook should provide a reason")
	}
}

func TestConfigHooksNil(t *testing.T) {
	var hooks *ConfigHooks
	blocked, _ := hooks.RunPreToolUse(context.Background(), "Bash", nil)
	if blocked {
		t.Error("nil hooks should not block")
	}
}

func TestConfigHooksEmptyMatcher(t *testing.T) {
	hooks := &ConfigHooks{
		PreToolUse: []HookEntry{
			{
				Matcher: "",
				Hooks: []HookCommand{
					{Type: "command", Command: "exit 0", Timeout: 5},
				},
			},
		},
	}

	blocked, _ := hooks.RunPreToolUse(context.Background(), "AnyTool", nil)
	if blocked {
		t.Error("empty matcher with exit 0 should not block")
	}
}

func TestParseConfigHooks(t *testing.T) {
	raw := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "echo hello",
							"timeout": 30,
						},
					},
				},
			},
		},
	}

	hooks := ParseConfigHooks(raw)
	if hooks == nil {
		t.Fatal("expected non-nil hooks")
	}
	if len(hooks.PreToolUse) != 1 {
		t.Fatalf("expected 1 PreToolUse entry, got %d", len(hooks.PreToolUse))
	}
	if hooks.PreToolUse[0].Matcher != "Bash" {
		t.Errorf("expected matcher 'Bash', got %q", hooks.PreToolUse[0].Matcher)
	}
	if len(hooks.PreToolUse[0].Hooks) != 1 {
		t.Fatalf("expected 1 hook command, got %d", len(hooks.PreToolUse[0].Hooks))
	}
	if hooks.PreToolUse[0].Hooks[0].Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", hooks.PreToolUse[0].Hooks[0].Command)
	}
}

func TestParseConfigHooksNoHooks(t *testing.T) {
	raw := map[string]any{
		"model": "test",
	}
	hooks := ParseConfigHooks(raw)
	if hooks != nil {
		t.Error("expected nil hooks when no hooks key")
	}
}
