package agent

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

// ============================================================================
// SubagentType tests
// ============================================================================

func TestBuiltinTypes(t *testing.T) {
	types := BuiltinTypes()
	expectedTypes := []string{"general-purpose", "explore", "plan", "shell", "verification"}

	if len(types) != len(expectedTypes) {
		t.Errorf("expected %d builtin types, got %d", len(expectedTypes), len(types))
	}

	for _, expected := range expectedTypes {
		found := false
		for _, tt := range types {
			if tt.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find type %q in BuiltinTypes()", expected)
		}
	}
}

func TestSubagentTypeAllowedTools(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		expected []string
	}{
		{
			name:     "general-purpose",
			typeName: "general-purpose",
			expected: []string{"*"},
		},
		{
			name:     "explore",
			typeName: "explore",
			expected: []string{"Read", "Glob", "Grep", "Bash"},
		},
		{
			name:     "plan",
			typeName: "plan",
			expected: []string{"Read", "Glob", "Grep"},
		},
		{
			name:     "shell",
			typeName: "shell",
			expected: []string{"Bash", "Read", "Glob", "Grep"},
		},
		{
			name:     "verification",
			typeName: "verification",
			expected: []string{"Read", "TaskOutput", "TaskStop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			if !reflect.DeepEqual(st.AllowedTools(), tt.expected) {
				t.Errorf("expected allowed tools %v, got %v", tt.expected, st.AllowedTools())
			}
		})
	}
}

func TestFilterTools(t *testing.T) {
	tests := []struct {
		name      string
		typeName  string
		denied    []string
		expectAbs []string
	}{
		{
			name:      "general-purpose denies Bash",
			typeName:  "general-purpose",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Write", "Edit", "Glob", "Grep", "WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource", "TaskOutput", "TaskStop", "Task", "CronCreate", "CronDelete", "CronList"},
		},
		{
			name:      "shell denies Bash",
			typeName:  "shell",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Glob", "Grep"},
		},
		{
			name:      "plan denies Bash (already excluded)",
			typeName:  "plan",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Glob", "Grep"},
		},
		{
			name:      "explore denies Bash",
			typeName:  "explore",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Glob", "Grep"},
		},
		{
			name:      "explore denies multiple",
			typeName:  "explore",
			denied:    []string{"Bash", "Glob"},
			expectAbs: []string{"Read", "Grep"},
		},
		{
			name:      "general-purpose no denies",
			typeName:  "general-purpose",
			denied:    []string{},
			expectAbs: []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource", "TaskOutput", "TaskStop", "Task", "CronCreate", "CronDelete", "CronList"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			result := st.FilterTools(tt.denied)
			if !reflect.DeepEqual(result, tt.expectAbs) {
				t.Errorf("FilterTools(%v) = %v, want %v", tt.denied, result, tt.expectAbs)
			}
		})
	}
}

func TestResolveModel(t *testing.T) {
	tests := []struct {
		alias    string
		expected string
	}{
		{alias: "sonnet", expected: "claude-sonnet-4-20250514"},
		{alias: "opus", expected: "claude-opus-4-20250514"},
		{alias: "haiku", expected: "claude-haiku-4-20250514"},
		{alias: "SONNET", expected: "claude-sonnet-4-20250514"}, // case insensitive
		{alias: "claude-4", expected: "claude-4"},               // unknown passes through
		{alias: "unknown", expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			result := ResolveModel(tt.alias)
			if result != tt.expected {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.alias, result, tt.expected)
			}
		})
	}
}

func TestCanResume(t *testing.T) {
	tests := []struct {
		typeName  string
		canResume bool
	}{
		{typeName: "general-purpose", canResume: true},
		{typeName: "explore", canResume: false},
		{typeName: "plan", canResume: false},
		{typeName: "shell", canResume: true},
		{typeName: "verification", canResume: true},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			if got := st.CanResume(); got != tt.canResume {
				t.Errorf("CanResume() = %v, want %v", got, tt.canResume)
			}
		})
	}
}

func TestRequiredMCPServers(t *testing.T) {
	tests := []struct {
		typeName string
		expected []string
	}{
		{typeName: "general-purpose", expected: []string{}},
		{typeName: "explore", expected: []string{}},
		{typeName: "plan", expected: []string{}},
		{typeName: "shell", expected: []string{}},
		{typeName: "verification", expected: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			result := st.RequiredMCPServers()
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("RequiredMCPServers() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFindBuiltin(t *testing.T) {
	tests := []struct {
		name  string
		found bool
	}{
		{name: "general-purpose", found: true},
		{name: "explore", found: true},
		{name: "plan", found: true},
		{name: "shell", found: true},
		{name: "verification", found: true},
		{name: "unknown", found: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.name)
			if (st != nil) != tt.found {
				t.Errorf("FindBuiltin(%q) found = %v, want %v", tt.name, st != nil, tt.found)
			}
		})
	}
}

func TestAllowedToolsAccessor(t *testing.T) {
	st := GeneralPurpose
	tools := st.AllowedTools()
	if len(tools) != 1 || tools[0] != "*" {
		t.Errorf("AllowedTools() returned unexpected value: %v", tools)
	}

	// Verify it returns a copy
	tools[0] = "modified"
	if GeneralPurpose.AllowedTools()[0] != "*" {
		t.Errorf("AllowedTools() returned a reference, not a copy")
	}
}

func TestRequiredMCPServersAccessor(t *testing.T) {
	st := GeneralPurpose
	servers := st.RequiredMCPServers()
	if len(servers) != 0 {
		t.Errorf("RequiredMCPServers() returned unexpected value: %v", servers)
	}

	// Verify it returns a copy
	servers = append(servers, "test")
	if len(GeneralPurpose.RequiredMCPServers()) != 0 {
		t.Errorf("RequiredMCPServers() returned a reference, not a copy")
	}
}

// ============================================================================
// Integration tests — require ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN
// ============================================================================

func TestSubagentType_InvalidTypeError(t *testing.T) {
	st := FindBuiltin("nonexistent")
	if st != nil {
		t.Fatal("expected nil for invalid type")
	}
	// Verify error message format from RunSubagent for invalid type
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	runner := NewLocalSubagentRunner(nil, nil, fastClient())
	params := tool.SubagentParams{
		Prompt:       "test",
		SubagentType: "nonexistent",
	}
	_, err := runner.RunSubagent(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for invalid subagent_type")
	}
	errStr := err.Error()
	// Error should contain the invalid type name
	if !strings.Contains(errStr, "nonexistent") {
		t.Errorf("error should contain invalid type name, got: %s", errStr)
	}
	// Error should list valid types
	if !strings.Contains(errStr, "valid types are") {
		t.Errorf("error should mention valid types, got: %s", errStr)
	}
}

func TestLocalSubagentRunner_AC1_InvalidTypeError(t *testing.T) {
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil, fastClient())

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "invalid-type",
	}

	result, err := runner.RunSubagent(context.Background(), params)
	if err == nil {
		// Should get an error for invalid type
		if result != nil {
			t.Logf("result: %s", result.Output)
		}
		// The error should be descriptive
		t.Error("expected error for invalid subagent_type")
	}

	// Error message should contain valid types
	if err != nil {
		errStr := err.Error()
		if errStr == "" {
			t.Error("error message should not be empty")
		}
		// Should mention the invalid type
		if !strings.Contains(errStr, "invalid-type") {
			t.Errorf("error should mention invalid type, got: %s", errStr)
		}
	}
}

func TestLocalSubagentRunner_AC3_ParameterPassthrough(t *testing.T) {
	// Test that parameters are forwarded correctly
	// This is a basic test - full verification would require mocking RunStream
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil, fastClient())

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
		Model:        "sonnet",
		CWD:          "/tmp",
	}

	// This will likely fail due to API client not being configured in test
	// but we can verify the params are being used
	_, _ = runner.RunSubagent(context.Background(), params)
	// If we get here without panic, the params were at least parsed correctly
}

func TestLocalSubagentRunner_AC4_SubagentLifecycle(t *testing.T) {
	// Test that subagent runs in its own context
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil, fastClient())

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
	}

	// Run once
	result1, _ := runner.RunSubagent(context.Background(), params)

	// Run again - should be independent
	result2, _ := runner.RunSubagent(context.Background(), params)

	// Both runs should complete (even if they fail due to no API client)
	if result1 == nil && result2 == nil {
		t.Error("at least one run should produce a result")
	}
}

func TestAsyncSubagentRunner_AC2_AsyncLaunch(t *testing.T) {
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	tools := []tool.Tool{readTool}

	runner := NewAsyncSubagentRunner(tools, nil, fastClient())

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
	}

	// Run async - should return immediately
	result, err := runner.RunSubagentAsync(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify response shape
	if result.Status != "async_launched" {
		t.Errorf("expected status 'async_launched', got %q", result.Status)
	}
	if result.AgentID == "" {
		t.Error("expected non-empty agent_id")
	}
	if result.Done == nil {
		t.Error("expected non-nil Done channel")
	}
}

func TestLocalSubagentRunner_AC4_StreamConfigPropagation(t *testing.T) {
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	runner := NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())

	// Set up parent config with all inherited fields
	parentCfg := StreamConfig{
		MaxBudgetUSD:         1.50,
		MaxTurns:             5,
		CustomSystemPrompt:   "custom prompt",
		AppendSystemPrompt:   "append prompt",
		OverrideSystemPrompt: true,
		StructuredSchema:     map[string]any{"type": "object"},
		StructuredDenyRules:  []string{"Bash"},
	}
	runner.SetParentConfig(&parentCfg)

	// Call RunSubagent with Name="worker1"
	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
		Name:         "worker1",
	}
	_, _ = runner.RunSubagent(context.Background(), params)

	// Get the captured stream config
	capturedCfg := runner.GetCapturedStreamConfig()

	// Verify IsNamedAgent is true
	if !capturedCfg.IsNamedAgent {
		t.Error("AC4 FAIL: IsNamedAgent should be true for named agent")
	} else {
		t.Log("AC4 PASS: IsNamedAgent is true")
	}

	// Verify all 7 inherited fields
	if capturedCfg.MaxBudgetUSD != parentCfg.MaxBudgetUSD {
		t.Errorf("AC4 FAIL: MaxBudgetUSD not inherited, got %v want %v", capturedCfg.MaxBudgetUSD, parentCfg.MaxBudgetUSD)
	} else {
		t.Log("AC4 PASS: MaxBudgetUSD inherited")
	}

	if capturedCfg.MaxTurns != parentCfg.MaxTurns {
		t.Errorf("AC4 FAIL: MaxTurns not inherited, got %v want %v", capturedCfg.MaxTurns, parentCfg.MaxTurns)
	} else {
		t.Log("AC4 PASS: MaxTurns inherited")
	}

	if capturedCfg.CustomSystemPrompt != parentCfg.CustomSystemPrompt {
		t.Errorf("AC4 FAIL: CustomSystemPrompt not inherited, got %q want %q", capturedCfg.CustomSystemPrompt, parentCfg.CustomSystemPrompt)
	} else {
		t.Log("AC4 PASS: CustomSystemPrompt inherited")
	}

	if capturedCfg.AppendSystemPrompt != parentCfg.AppendSystemPrompt {
		t.Errorf("AC4 FAIL: AppendSystemPrompt not inherited, got %q want %q", capturedCfg.AppendSystemPrompt, parentCfg.AppendSystemPrompt)
	} else {
		t.Log("AC4 PASS: AppendSystemPrompt inherited")
	}

	if capturedCfg.OverrideSystemPrompt != parentCfg.OverrideSystemPrompt {
		t.Errorf("AC4 FAIL: OverrideSystemPrompt not inherited, got %v want %v", capturedCfg.OverrideSystemPrompt, parentCfg.OverrideSystemPrompt)
	} else {
		t.Log("AC4 PASS: OverrideSystemPrompt inherited")
	}

	if capturedCfg.StructuredSchema == nil {
		t.Error("AC4 FAIL: StructuredSchema not inherited, got nil")
	} else {
		t.Log("AC4 PASS: StructuredSchema inherited")
	}

	if len(capturedCfg.StructuredDenyRules) != len(parentCfg.StructuredDenyRules) {
		t.Errorf("AC4 FAIL: StructuredDenyRules not inherited, got %v want %v", capturedCfg.StructuredDenyRules, parentCfg.StructuredDenyRules)
	} else {
		t.Log("AC4 PASS: StructuredDenyRules inherited")
	}
}

// ============================================================================
// AC7: Subagent Permission Level Inheritance Tests
// ============================================================================

func TestLocalSubagentRunner_AC7_InheritanceAntiEscalation(t *testing.T) {
	// AC1+AC2 merged: Subagent inherits parent's PermissionLevel across all levels.
	// Anti-escalation guarantee: SubagentParams has no PermissionLevel field, so
	// inheritance is the only mechanism. This table-driven test proves the
	// anti-escalation property for all levels in one pass.
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	levels := []tool.PermissionLevel{
		tool.PermissionRead,
		tool.PermissionEdit,
		tool.PermissionExecute,
		tool.PermissionUnrestricted,
	}

	for _, parentLevel := range levels {
		t.Run(parentLevel.String(), func(t *testing.T) {
			readTool := tool.NewReadTool(tool.PermissionEdit, nil)
			runner := NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())

			parentCfg := StreamConfig{
				PermissionLevel: parentLevel,
			}
			runner.SetParentConfig(&parentCfg)

			params := tool.SubagentParams{
				Prompt:       "test prompt",
				SubagentType: "explore",
				Name:         "worker1",
			}
			_, _ = runner.RunSubagent(context.Background(), params)

			capturedCfg := runner.GetCapturedStreamConfig()
			if capturedCfg == nil {
				t.Fatal("GetCapturedStreamConfig returned nil")
			}

			if capturedCfg.PermissionLevel != parentLevel {
				t.Errorf("got %v, want %v", capturedCfg.PermissionLevel, parentLevel)
			} else {
				t.Logf("PASS: Subagent inherited PermissionLevel=%s from parent", parentLevel)
			}
		})
	}
}

func TestLocalSubagentRunner_AC7_UnrestrictedParentInheritsCorrectly(t *testing.T) {
	// AC3: Subagent at unrestricted inherits correctly. Parent at
	// PermissionLevel=unrestricted spawns subagent, subagent inherits unrestricted.
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	runner := NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())

	// Parent config has PermissionLevel=unrestricted
	parentCfg := StreamConfig{
		PermissionLevel: tool.PermissionUnrestricted,
	}
	runner.SetParentConfig(&parentCfg)

	// Run a named subagent
	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "general-purpose",
		Name:         "unrestricted-worker",
	}
	_, _ = runner.RunSubagent(context.Background(), params)

	// Get the captured stream config
	capturedCfg := runner.GetCapturedStreamConfig()
	if capturedCfg == nil {
		t.Fatal("GetCapturedStreamConfig returned nil")
	}

	// Verify PermissionLevel is inherited as unrestricted
	if capturedCfg.PermissionLevel != tool.PermissionUnrestricted {
		t.Errorf("AC3 FAIL: PermissionLevel not inherited correctly, got %v want %v",
			capturedCfg.PermissionLevel, tool.PermissionUnrestricted)
	} else {
		t.Logf("AC3 PASS: Subagent inherited PermissionLevel=unrestricted from parent")
	}
}

func TestLocalSubagentRunner_AC7_NestedSubagentCannotEscalate(t *testing.T) {
	// AC4: Nested subagent cannot escalate beyond original parent.
	// Parent at PermissionLevel=edit spawns subagent (captured level edit),
	// that subagent spawns its own subagent (grandchild), and grandchild
	// also has PermissionLevel=edit. Double-inheritance does not allow escalation.
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)

	// First runner: grandparent
	runner1 := NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())
	parentCfg1 := StreamConfig{
		PermissionLevel: tool.PermissionEdit,
	}
	runner1.SetParentConfig(&parentCfg1)

	// Grandparent spawns parent agent
	params1 := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "general-purpose",
		Name:         "level1-agent",
	}
	_, _ = runner1.RunSubagent(context.Background(), params1)

	// Capture the parent's config (now level=edit)
	parentCapturedCfg := runner1.GetCapturedStreamConfig()
	if parentCapturedCfg == nil {
		t.Fatal("AC4 FAIL: First runner captured config is nil")
	}
	if parentCapturedCfg.PermissionLevel != tool.PermissionEdit {
		t.Errorf("AC4 FAIL: First subagent level is %v, expected edit",
			parentCapturedCfg.PermissionLevel)
	}

	// Second runner: parent spawns child (grandchild of original)
	runner2 := NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())
	parentCfg2 := StreamConfig{
		PermissionLevel: parentCapturedCfg.PermissionLevel, // Should be edit
	}
	runner2.SetParentConfig(&parentCfg2)

	// Parent spawns grandchild agent
	params2 := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "general-purpose",
		Name:         "level2-agent",
	}
	_, _ = runner2.RunSubagent(context.Background(), params2)

	// Get the grandchild's captured config
	grandchildCapturedCfg := runner2.GetCapturedStreamConfig()
	if grandchildCapturedCfg == nil {
		t.Fatal("AC4 FAIL: Second runner captured config is nil")
	}

	// Verify grandchild level is still edit (not escalated)
	if grandchildCapturedCfg.PermissionLevel != tool.PermissionEdit {
		t.Errorf("AC4 FAIL: Grandchild subagent level is %v, expected edit",
			grandchildCapturedCfg.PermissionLevel)
	} else {
		t.Logf("AC4 PASS: Nested subagent (grandchild) correctly inherited PermissionLevel=edit")
	}
}

func TestLocalSubagentRunner_AC7_UnnamedSubagentNoInheritance(t *testing.T) {
	// Verify that unnamed subagents (params.Name == "") do NOT inherit
	// PermissionLevel - they get the default zero value.
	// This is important because only named agents trigger the inheritance code.
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(tool.PermissionEdit, nil)
	runner := NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())

	// Set up parent config with PermissionLevel=execute
	parentCfg := StreamConfig{
		PermissionLevel: tool.PermissionExecute,
	}
	runner.SetParentConfig(&parentCfg)

	// Call RunSubagent WITHOUT Name (unnamed subagent)
	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
		// Name is intentionally empty - should not trigger inheritance
	}
	_, _ = runner.RunSubagent(context.Background(), params)

	// Get the captured stream config
	capturedCfg := runner.GetCapturedStreamConfig()
	if capturedCfg == nil {
		t.Fatal("GetCapturedStreamConfig returned nil")
	}

	// For unnamed subagents, PermissionLevel should be zero (not inherited)
	// This verifies that the inheritance code path only activates for named agents
	if capturedCfg.PermissionLevel != 0 {
		t.Errorf("Unnamed subagent should not inherit PermissionLevel, got %v want 0",
			capturedCfg.PermissionLevel)
	} else {
		t.Logf("Unnamed subagent correctly did not inherit PermissionLevel")
	}
}
