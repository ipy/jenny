package tool

import (
	"context"
	"testing"
)

func TestTodoWriteTool_Name(t *testing.T) {
	tool := NewTodoWriteTool()
	if got := tool.Name(); got != "TodoWrite" {
		t.Errorf("Name() = %v, want %v", got, "TodoWrite")
	}
}

func TestTodoWriteTool_InputSchema(t *testing.T) {
	tool := NewTodoWriteTool()
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Errorf("InputSchema() type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() properties not a map")
	}

	// Check required fields
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("InputSchema() required not a slice")
	}

	hasAction := false
	hasSubject := false
	for _, r := range required {
		if r == "action" {
			hasAction = true
		}
		if r == "subject" {
			hasSubject = true
		}
	}
	if !hasAction || !hasSubject {
		t.Errorf("InputSchema() missing required fields: action=%v, subject=%v", hasAction, hasSubject)
	}

	// Check action enum
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() action property not found")
	}
	enum, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("InputSchema() action enum not found")
	}
	if len(enum) != 3 || enum[0] != "create" || enum[1] != "update" || enum[2] != "complete" {
		t.Errorf("InputSchema() action enum = %v, want [create, update, complete]", enum)
	}
}

func TestTodoWriteTool_Execute(t *testing.T) {
	tool := NewTodoWriteTool()
	ctx := context.Background()

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name: "create todo item",
			input: map[string]any{
				"action":     "create",
				"subject":    "test-subject",
				"activeForm": "Implementing test feature",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					r.Content == "Created todo: Implementing test feature"
			},
		},
		{
			name: "create another todo in same subject",
			input: map[string]any{
				"action":     "create",
				"subject":    "test-subject",
				"activeForm": "Another task",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					r.Content == "Created todo: Another task"
			},
		},
		{
			name: "update todo item",
			input: map[string]any{
				"action":     "update",
				"subject":    "test-subject",
				"activeForm": "Updated task description",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					r.Content == "Updated todo: Updated task description"
			},
		},
		{
			name: "complete first todo",
			input: map[string]any{
				"action":  "complete",
				"subject": "test-subject",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					r.Content == "Completed todo for: test-subject"
			},
		},
		{
			name: "complete second todo - all done should clear list",
			input: map[string]any{
				"action":  "complete",
				"subject": "test-subject",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					r.Content == "All todos completed for: test-subject"
			},
		},
		{
			name: "complete non-existent subject",
			input: map[string]any{
				"action":  "complete",
				"subject": "non-existent",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError &&
					r.Content == "todo not found: non-existent"
			},
		},
		{
			name: "update non-existent subject",
			input: map[string]any{
				"action":     "update",
				"subject":    "non-existent",
				"activeForm": "Some action",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError &&
					r.Content == "todo not found: non-existent"
			},
		},
		{
			name: "create with missing action",
			input: map[string]any{
				"subject":    "test",
				"activeForm": "test",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError &&
					r.Content == "action is required (create, update, complete)"
			},
		},
		{
			name: "create with missing subject",
			input: map[string]any{
				"action":     "create",
				"activeForm": "test",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError &&
					r.Content == "subject is required"
			},
		},
		{
			name: "unknown action",
			input: map[string]any{
				"action":  "delete",
				"subject": "test",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError &&
					r.Content == "unknown action: delete. Use create, update, or complete"
			},
		},
		{
			name: "complete when all already completed",
			input: map[string]any{
				"action":  "complete",
				"subject": "pre-cleared-subject",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError &&
					r.Content == "todo not found: pre-cleared-subject"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.input, "/tmp")
			if err != nil {
				t.Errorf("Execute() unexpected error = %v", err)
				return
			}
			if (result != nil && result.IsError) != tt.wantErr {
				t.Errorf("Execute() result.IsError = %v, wantErr %v, content = %s", result != nil && result.IsError, tt.wantErr, result.Content)
				return
			}
			if tt.checkFn != nil && !tt.checkFn(result) {
				t.Errorf("Execute() result = %v, check failed", result)
			}
		})
	}
}

func TestTodoWriteTool_AutoClear(t *testing.T) {
	tool := NewTodoWriteTool()
	ctx := context.Background()

	// Create two todos
	_, err := tool.Execute(ctx, map[string]any{
		"action":     "create",
		"subject":    "clear-me",
		"activeForm": "First task",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	_, err = tool.Execute(ctx, map[string]any{
		"action":     "create",
		"subject":    "clear-me",
		"activeForm": "Second task",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Complete first
	result, err := tool.Execute(ctx, map[string]any{
		"action":  "complete",
		"subject": "clear-me",
	}, "/tmp")
	if err != nil || result.IsError {
		t.Fatalf("First complete failed: error=%v, result=%v", err, result)
	}

	// Complete second - should auto-clear
	result, err = tool.Execute(ctx, map[string]any{
		"action":  "complete",
		"subject": "clear-me",
	}, "/tmp")
	if err != nil || result.IsError {
		t.Fatalf("Second complete failed: error=%v, result=%v", err, result)
	}

	if result.Content != "All todos completed for: clear-me" {
		t.Errorf("Expected auto-clear message, got: %s", result.Content)
	}

	// Verify the list is cleared by trying to complete again
	result, err = tool.Execute(ctx, map[string]any{
		"action":  "complete",
		"subject": "clear-me",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() unexpected error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Third complete should return error after auto-clear, got: %s", result.Content)
	}
	if result.Content != "todo not found: clear-me" {
		t.Errorf("Expected 'todo not found: clear-me', got: %s", result.Content)
	}
}
