package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTaskUpdateTool_Name(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskUpdateTool(store)
	if got := tool.Name(); got != "TaskUpdate" {
		t.Errorf("Name() = %v, want %v", got, "TaskUpdate")
	}
}

func TestTaskUpdateTool_InputSchema(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskUpdateTool(store)
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

	// Check all expected fields exist
	expectedFields := []string{"task_id", "subject", "description", "active_form", "status", "metadata", "add_blocks", "add_blocked_by"}
	for _, field := range expectedFields {
		if _, ok := props[field]; !ok {
			t.Errorf("InputSchema() missing property: %s", field)
		}
	}
}

func TestTaskUpdateTool_Execute(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskUpdateTool(store)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name: "update subject",
			input: map[string]any{
				"task_id": createUpdateTestTask(t, store),
				"subject": "updated subject",
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
				return result["subject"] == "updated subject"
			},
		},
		{
			name: "update description",
			input: map[string]any{
				"task_id":     createUpdateTestTask(t, store),
				"description": "new description",
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
				return result["description"] == "new description"
			},
		},
		{
			name: "update active_form",
			input: map[string]any{
				"task_id":     createUpdateTestTask(t, store),
				"active_form": "working on it",
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
				return result["active_form"] == "working on it"
			},
		},
		{
			name: "update status to in_progress",
			input: map[string]any{
				"task_id": createUpdateTestTask(t, store),
				"status":  "in_progress",
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
				return result["status"] == "in_progress"
			},
		},
		{
			name: "update status to completed",
			input: map[string]any{
				"task_id": createUpdateTestTask(t, store),
				"status":  "completed",
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
				return result["status"] == "completed"
			},
		},
		{
			name: "update status to deleted",
			input: map[string]any{
				"task_id": createUpdateTestTask(t, store),
				"status":  "deleted",
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
				return result["status"] == "deleted"
			},
		},
		{
			name: "update multiple fields at once",
			input: map[string]any{
				"task_id":     createUpdateTestTask(t, store),
				"subject":     "multi-update",
				"description": "multi-desc",
				"active_form": "multi-form",
				"status":      "in_progress",
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
				return result["subject"] == "multi-update" &&
					result["description"] == "multi-desc" &&
					result["active_form"] == "multi-form" &&
					result["status"] == "in_progress"
			},
		},
		{
			name: "update metadata merge adds keys",
			input: map[string]any{
				"task_id":  createUpdateTestTaskWithMeta(t, store, map[string]any{"existing": "value"}),
				"metadata": map[string]any{"newkey": "newvalue"},
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
				metadata := result["metadata"].(map[string]any)
				return metadata["existing"] == "value" && metadata["newkey"] == "newvalue"
			},
		},
		{
			name: "update metadata null deletes key",
			input: map[string]any{
				"task_id":  createUpdateTestTaskWithMeta(t, store, map[string]any{"todelete": "value", "tokeep": "value"}),
				"metadata": map[string]any{"todelete": nil},
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
				metadata := result["metadata"].(map[string]any)
				if _, ok := metadata["todelete"]; ok {
					return false // todelete should be gone
				}
				if metadata["tokeep"] != "value" {
					return false // tokeep should remain
				}
				return true
			},
		},
		{
			name: "add_blocks updates dependency graph",
			input: map[string]any{
				"task_id":    createUpdateTestTask(t, store),
				"add_blocks": []any{"blocker-id-1", "blocker-id-2"},
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
				blocks := result["blocks"].([]any)
				return len(blocks) == 2
			},
		},
		{
			name: "add_blocked_by updates dependency graph",
			input: map[string]any{
				"task_id":        createUpdateTestTask(t, store),
				"add_blocked_by": []any{"blocked-by-1", "blocked-by-2"},
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
				blockedBy := result["blocked_by"].([]any)
				return len(blockedBy) == 2
			},
		},
		{
			name:    "update without task_id returns error",
			input:   map[string]any{},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "task_id is required"
			},
		},
		{
			name: "update nonexistent task returns error",
			input: map[string]any{
				"task_id": "nonexistent",
				"subject": "new subject",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "task not found"
			},
		},
		{
			name: "update with invalid status returns error",
			input: map[string]any{
				"task_id": createUpdateTestTask(t, store),
				"status":  "invalid_status",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "invalid status: invalid_status (must be pending, in_progress, completed, or deleted)"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.input, t.TempDir())
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

func TestTaskUpdateTool_UpdatedAt(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskUpdateTool(store)
	ctx := context.Background()

	taskID := createUpdateTestTask(t, store)
	before := time.Now().Add(-time.Second)

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": taskID,
		"subject": "updated",
	}, t.TempDir())
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

	updatedAtStr, ok := task["updated_at"].(string)
	if !ok {
		t.Fatalf("updated_at not a string")
	}

	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		t.Fatalf("Failed to parse updated_at: %v", err)
	}

	after := time.Now().Add(time.Second)
	if updatedAt.Before(before) || updatedAt.After(after) {
		t.Errorf("UpdatedAt %v not in expected range [%v, %v]", updatedAt, before, after)
	}
}

func TestTaskUpdateTool_MetadataMergeOverwrite(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskUpdateTool(store)
	ctx := context.Background()

	taskID := createUpdateTestTaskWithMeta(t, store, map[string]any{
		"key1": "original1",
		"key2": "original2",
	})

	result, err := tool.Execute(ctx, map[string]any{
		"task_id":  taskID,
		"metadata": map[string]any{"key1": "updated1", "key3": "new3"},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var task map[string]any
	if err := json.Unmarshal([]byte(result.Content), &task); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	metadata := task["metadata"].(map[string]any)
	if metadata["key1"] != "updated1" {
		t.Errorf("key1 should be updated to 'updated1', got: %v", metadata["key1"])
	}
	if metadata["key2"] != "original2" {
		t.Errorf("key2 should remain 'original2', got: %v", metadata["key2"])
	}
	if metadata["key3"] != "new3" {
		t.Errorf("key3 should be 'new3', got: %v", metadata["key3"])
	}
}

// createUpdateTestTask is a helper that creates a task and returns its ID.
func createUpdateTestTask(t *testing.T, store *TaskStore) string {
	t.Helper()
	task, err := store.Create("update-test", "original desc", "original form", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return task.ID
}

// createUpdateTestTaskWithMeta creates a task with initial metadata.
func createUpdateTestTaskWithMeta(t *testing.T, store *TaskStore, metadata map[string]any) string {
	t.Helper()
	task, err := store.Create("update-test-meta", "desc", "form", metadata)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return task.ID
}
