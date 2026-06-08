package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTaskGetTool_Name(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskGetTool(store)
	if got := tool.Name(); got != "TaskGet" {
		t.Errorf("Name() = %v, want %v", got, "TaskGet")
	}
}

func TestTaskGetTool_InputSchema(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskGetTool(store)
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Errorf("InputSchema() type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() properties not a map")
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("InputSchema() required not a slice")
	}

	hasTaskID := false
	for _, r := range required {
		if r == "task_id" {
			hasTaskID = true
		}
	}
	if !hasTaskID {
		t.Errorf("InputSchema() missing required field: task_id")
	}

	if _, ok := props["task_id"]; !ok {
		t.Errorf("InputSchema() missing task_id property")
	}
}

func TestTaskGetTool_Execute(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskGetTool(store)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name: "get existing task returns full record",
			input: map[string]any{
				"task_id": createTestTask(t, store, "test-task", "description", "doing stuff", map[string]any{"priority": "high"}),
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				if r == nil || r.IsError {
					return false
				}
				var result map[string]any
				if err := json.Unmarshal([]byte(r.Content), &result); err != nil {
					return false
				}
				return result["id"] != nil &&
					result["subject"] == "test-task" &&
					result["description"] == "description" &&
					result["active_form"] == "doing stuff" &&
					result["status"] == "pending" &&
					result["created_at"] != nil &&
					result["updated_at"] != nil
			},
		},
		{
			name:    "get missing task returns not found gracefully",
			input:   map[string]any{"task_id": "nonexistent"},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				if r == nil {
					return false
				}
				return !r.IsError && r.Content == "task not found"
			},
		},
		{
			name:    "get without task_id returns error",
			input:   map[string]any{},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "task_id is required"
			},
		},
		{
			name: "get with empty task_id returns error",
			input: map[string]any{
				"task_id": "",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "task_id is required"
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

func TestTaskGetTool_Timestamps(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskGetTool(store)
	ctx := context.Background()

	taskID := createTestTask(t, store, "timestamp-test", "", "", nil)

	result, err := tool.Execute(ctx, map[string]any{"task_id": taskID}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() result.IsError = true")
	}

	var task map[string]any
	if err := json.Unmarshal([]byte(result.Content), &task); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	createdAtStr, ok := task["created_at"].(string)
	if !ok {
		t.Fatalf("created_at not a string")
	}
	createdAt, err := time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		t.Fatalf("Failed to parse created_at: %v", err)
	}

	updatedAtStr, ok := task["updated_at"].(string)
	if !ok {
		t.Fatalf("updated_at not a string")
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		t.Fatalf("Failed to parse updated_at: %v", err)
	}

	if updatedAt.Before(createdAt) {
		t.Errorf("updated_at %v should not be before created_at %v", updatedAt, createdAt)
	}
}

// createTestTask is a helper that creates a task and returns its ID.
func createTestTask(t *testing.T, store *TaskStore, subject, desc, activeForm string, metadata map[string]any) string {
	t.Helper()
	task, err := store.Create(subject, desc, activeForm, metadata)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return task.ID
}
