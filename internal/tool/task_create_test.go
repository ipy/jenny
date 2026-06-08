package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTaskCreateTool_Name(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskCreateTool(store)
	if got := tool.Name(); got != "TaskCreate" {
		t.Errorf("Name() = %v, want %v", got, "TaskCreate")
	}
}

func TestTaskCreateTool_InputSchema(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskCreateTool(store)
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

	hasSubject := false
	for _, r := range required {
		if r == "subject" {
			hasSubject = true
		}
	}
	if !hasSubject {
		t.Errorf("InputSchema() missing required field: subject")
	}

	// Check optional fields exist
	if _, ok := props["description"]; !ok {
		t.Errorf("InputSchema() missing description property")
	}
	if _, ok := props["active_form"]; !ok {
		t.Errorf("InputSchema() missing active_form property")
	}
	if _, ok := props["metadata"]; !ok {
		t.Errorf("InputSchema() missing metadata property")
	}
}

func TestTaskCreateTool_Execute(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskCreateTool(store)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name: "create task with subject only",
			input: map[string]any{
				"subject": "test-task",
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
				return result["id"] != nil && result["subject"] == "test-task"
			},
		},
		{
			name: "create task with all fields",
			input: map[string]any{
				"subject":     "full-task",
				"description": "A detailed description",
				"active_form": "Implementing full task",
				"metadata":    map[string]any{"priority": "high"},
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
				return result["subject"] == "full-task" &&
					result["description"] == "A detailed description" &&
					result["active_form"] == "Implementing full task" &&
					result["status"] == "pending"
			},
		},
		{
			name:    "create task without subject",
			input:   map[string]any{},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "subject is required"
			},
		},
		{
			name: "create task with empty subject",
			input: map[string]any{
				"subject": "",
			},
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && r.Content == "subject is required"
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

func TestTaskCreateTool_CreatedAt(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskCreateTool(store)
	ctx := context.Background()

	before := time.Now().Add(-time.Second)
	result, err := tool.Execute(ctx, map[string]any{
		"subject": "timestamp-test",
	}, "/tmp")
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

	after := time.Now().Add(time.Second)
	if createdAt.Before(before) || createdAt.After(after) {
		t.Errorf("CreatedAt %v not in expected range [%v, %v]", createdAt, before, after)
	}
}

func TestTaskStore(t *testing.T) {
	store := NewTaskStore()

	// Create
	task, err := store.Create("test subject", "test description", "testing", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.ID == "" {
		t.Errorf("Create() returned task with empty ID")
	}
	if task.Subject != "test subject" {
		t.Errorf("Create() subject = %v, want %v", task.Subject, "test subject")
	}
	if task.Status != TaskStatusPending {
		t.Errorf("Create() status = %v, want %v", task.Status, TaskStatusPending)
	}

	// Get
	retrieved := store.Get(task.ID)
	if retrieved == nil {
		t.Fatalf("Get() returned nil")
	}
	if retrieved.ID != task.ID {
		t.Errorf("Get() ID = %v, want %v", retrieved.ID, task.ID)
	}

	// List
	tasks := store.List(TaskFilter{})
	if len(tasks) != 1 {
		t.Errorf("List() returned %v tasks, want 1", len(tasks))
	}

	// Update
	updated := store.Update(task.ID, map[string]any{
		"subject": "updated subject",
		"status":  "in_progress",
	})
	if updated == nil {
		t.Fatalf("Update() returned nil")
	}
	if updated.Subject != "updated subject" {
		t.Errorf("Update() subject = %v, want %v", updated.Subject, "updated subject")
	}
	if updated.Status != TaskStatusInProgress {
		t.Errorf("Update() status = %v, want %v", updated.Status, TaskStatusInProgress)
	}

	// Delete
	store.Delete(task.ID)
	if store.Get(task.ID) != nil {
		t.Errorf("Get() after Delete() should return nil")
	}
}

func TestTaskStore_ListFilter(t *testing.T) {
	store := NewTaskStore()

	task1, _ := store.Create("task1", "", "", nil)
	task2, _ := store.Create("task2", "", "", nil)

	// Complete task1
	store.Update(task1.ID, map[string]any{"status": "completed"})

	// Filter by pending - should return task2
	pending := store.List(TaskFilter{Status: TaskStatusPending})
	if len(pending) != 1 {
		t.Errorf("List(pending) returned %v tasks, want 1", len(pending))
	}
	if pending[0].ID != task2.ID {
		t.Errorf("List(pending)[0].ID = %v, want %v", pending[0].ID, task2.ID)
	}

	// Filter by completed - should return task1
	completed := store.List(TaskFilter{Status: TaskStatusCompleted})
	if len(completed) != 1 {
		t.Errorf("List(completed) returned %v tasks, want 1", len(completed))
	}
	if completed[0].ID != task1.ID {
		t.Errorf("List(completed)[0].ID = %v, want %v", completed[0].ID, task1.ID)
	}
}
