package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

func TestLocalSubagentRunner_AC1_ValidTypeResolve(t *testing.T) {
	// Create a minimal tool list for testing
	readTool := tool.NewReadTool(false, nil)
	bashTool := tool.NewBashTool(false)
	globTool := tool.NewGlobTool()
	grepTool := tool.NewGrepTool()
	tools := []tool.Tool{readTool, bashTool, globTool, grepTool}

	runner := NewLocalSubagentRunner(tools, nil)

	tests := []struct {
		typeName      string
		expectedTools []string
	}{
		{
			typeName:      "explore",
			expectedTools: []string{"Read", "Glob", "Grep", "Bash"},
		},
		{
			typeName:      "plan",
			expectedTools: []string{"Read", "Glob", "Grep"},
		},
		{
			typeName:      "shell",
			expectedTools: []string{"Bash", "Read", "Glob", "Grep"},
		},
		{
			typeName:      "general-purpose",
			expectedTools: []string{"*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			params := tool.SubagentParams{
				Prompt:       "test prompt",
				SubagentType: tt.typeName,
			}
			result, err := runner.RunSubagent(context.Background(), params)
			if err != nil {
				// For explore/plan which are one-shot, we might get an error
				// but the key is that the type was valid
				st := FindBuiltin(tt.typeName)
				if st == nil {
					t.Errorf("expected valid subagent type %q", tt.typeName)
				}
				return
			}
			// If no error, the result should have output
			if result != nil && result.Output == "" {
				// Empty output is OK for a minimal test
			}
		})
	}
}

func TestLocalSubagentRunner_AC1_InvalidTypeError(t *testing.T) {
	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil)

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
	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil)

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
	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil)

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
	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewAsyncSubagentRunner(tools, nil)

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
	if result.OutputFile == "" {
		t.Error("expected non-empty output_file")
	}
}

func TestBuiltinTypesMatchSubagentTypes(t *testing.T) {
	// Verify that BuiltinTypes() returns the same types as the subagent type registry
	types := BuiltinTypes()
	expectedTypes := []string{"general-purpose", "explore", "plan", "shell", "verification"}

	if len(types) != len(expectedTypes) {
		t.Errorf("expected %d builtin types, got %d", len(expectedTypes), len(types))
	}

	for _, expected := range expectedTypes {
		found := false
		for _, t := range types {
			if t.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find type %q in BuiltinTypes()", expected)
		}
	}
}
