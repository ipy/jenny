package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTaskListTool_Name(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	if got := tool.Name(); got != "TaskList" {
		t.Errorf("Name() = %v, want %v", got, "TaskList")
	}
}

func TestTaskListTool_InputSchema(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Errorf("InputSchema() type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() properties not a map")
	}

	if _, ok := props["status"]; !ok {
		t.Errorf("InputSchema() missing status property")
	}
}

func TestTaskListTool_Execute(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name:  "list all tasks unfiltered",
			input: map[string]any{},
			checkFn: func(r *ToolResult) bool {
				if r == nil || r.IsError {
					return false
				}
				var result []map[string]any
				if err := json.Unmarshal([]byte(r.Content), &result); err != nil {
					return false
				}
				return len(result) >= 0 // empty is ok, just means no tasks
			},
		},
		{
			name:  "list tasks returns all tasks when no filter",
			input: map[string]any{},
			checkFn: func(r *ToolResult) bool {
				if r == nil || r.IsError {
					return false
				}
				// Create a task first
				_, err := store.Create("list-test-1", "", "", nil)
				if err != nil {
					return false
				}
				_, err = store.Create("list-test-2", "", "", nil)
				if err != nil {
					return false
				}

				// Re-execute to get the updated list
				result2, err := tool.Execute(ctx, map[string]any{}, "/tmp")
				if err != nil || result2.IsError {
					return false
				}
				var tasks []map[string]any
				if err := json.Unmarshal([]byte(result2.Content), &tasks); err != nil {
					return false
				}
				return len(tasks) >= 2
			},
		},
		{
			name:  "list filters by status",
			input: map[string]any{"status": "in_progress"},
			checkFn: func(r *ToolResult) bool {
				if r == nil || r.IsError {
					return false
				}
				var result []map[string]any
				if err := json.Unmarshal([]byte(r.Content), &result); err != nil {
					return false
				}
				for _, task := range result {
					if task["status"] != "in_progress" {
						return false
					}
				}
				return true
			},
		},
		{
			name:  "list filters internal metadata",
			input: map[string]any{},
			checkFn: func(r *ToolResult) bool {
				if r == nil || r.IsError {
					return false
				}
				var result []map[string]any
				if err := json.Unmarshal([]byte(r.Content), &result); err != nil {
					return false
				}
				for _, task := range result {
					metadata, ok := task["metadata"].(map[string]any)
					if !ok {
						continue
					}
					for k := range metadata {
						if len(k) > 0 && k[0] == '_' {
							return false // internal key found
						}
					}
				}
				return true
			},
		},
		{
			name:  "list strips resolved blockers from blockedBy",
			input: map[string]any{},
			checkFn: func(r *ToolResult) bool {
				if r == nil || r.IsError {
					return false
				}
				var result []map[string]any
				if err := json.Unmarshal([]byte(r.Content), &result); err != nil {
					return false
				}
				for _, task := range result {
					blockedBy, ok := task["blocked_by"].([]any)
					if !ok {
						continue
					}
					// blockedBy should only contain unresolved task IDs
					for _, b := range blockedBy {
						blockerID, ok := b.(string)
						if !ok {
							continue
						}
						blocker := store.Get(blockerID)
						if blocker != nil {
							if blocker.Status == TaskStatusCompleted || blocker.Status == TaskStatusDeleted {
								return false // resolved blocker found
							}
						}
					}
				}
				return true
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

func TestTaskListTool_StatusFilter(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	ctx := context.Background()

	// Create tasks with different statuses
	pendingTask, _ := store.Create("pending-task", "", "", nil)
	inProgressTask, _ := store.Create("in-progress-task", "", "", nil)
	completedTask, _ := store.Create("completed-task", "", "", nil)

	store.Update(pendingTask.ID, map[string]any{"status": "pending"})
	store.Update(inProgressTask.ID, map[string]any{"status": "in_progress"})
	store.Update(completedTask.ID, map[string]any{"status": "completed"})

	// Filter by pending
	result, err := tool.Execute(ctx, map[string]any{"status": "pending"}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var pending []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &pending); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if len(pending) != 1 || pending[0]["subject"] != "pending-task" {
		t.Errorf("pending filter returned wrong tasks: %v", pending)
	}

	// Filter by in_progress
	result, err = tool.Execute(ctx, map[string]any{"status": "in_progress"}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var inProgress []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &inProgress); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if len(inProgress) != 1 || inProgress[0]["subject"] != "in-progress-task" {
		t.Errorf("in_progress filter returned wrong tasks: %v", inProgress)
	}

	// Filter by completed
	result, err = tool.Execute(ctx, map[string]any{"status": "completed"}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var completed []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &completed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if len(completed) != 1 || completed[0]["subject"] != "completed-task" {
		t.Errorf("completed filter returned wrong tasks: %v", completed)
	}
}

func TestTaskListTool_InternalMetadataFilter(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	ctx := context.Background()

	// Create task with internal metadata
	_, err := store.Create("meta-test", "", "", map[string]any{
		"priority":  "high",
		"_internal": "secret",
		"_private":  "data",
		"public":    "info",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	result, err := tool.Execute(ctx, map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &tasks); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	found := false
	for _, task := range tasks {
		if task["subject"] == "meta-test" {
			found = true
			metadata := task["metadata"].(map[string]any)
			if _, ok := metadata["_internal"]; ok {
				t.Errorf("Internal metadata _internal should be filtered out: %v", metadata)
			}
			if _, ok := metadata["_private"]; ok {
				t.Errorf("Internal metadata _private should be filtered out: %v", metadata)
			}
			if metadata["priority"] != "high" {
				t.Errorf("public metadata priority should be preserved: %v", metadata)
			}
			if metadata["public"] != "info" {
				t.Errorf("public metadata public should be preserved: %v", metadata)
			}
		}
	}
	if !found {
		t.Errorf("meta-test task not found in list")
	}
}

func TestTaskListTool_ResolvedBlockersFiltered(t *testing.T) {
	store := NewTaskStore()
	tool := NewTaskListTool(store)
	ctx := context.Background()

	// Create blocker tasks
	activeBlocker, _ := store.Create("active-blocker", "", "", nil)
	completedBlocker, _ := store.Create("completed-blocker", "", "", nil)
	deletedBlocker, _ := store.Create("deleted-blocker", "", "", nil)

	store.Update(completedBlocker.ID, map[string]any{"status": "completed"})
	store.Update(deletedBlocker.ID, map[string]any{"status": "deleted"})

	// Create task blocked by all three
	blockedTask, _ := store.Create("blocked-task", "", "", nil)
	store.AddDependencies(blockedTask.ID, nil, []string{activeBlocker.ID, completedBlocker.ID, deletedBlocker.ID})

	result, err := tool.Execute(ctx, map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &tasks); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	for _, task := range tasks {
		if task["subject"] == "blocked-task" {
			blockedBy := task["blocked_by"].([]any)
			if len(blockedBy) != 1 {
				t.Errorf("blockedBy should have 1 entry (only active blocker), got %d: %v", len(blockedBy), blockedBy)
			}
			if len(blockedBy) > 0 && blockedBy[0].(string) != activeBlocker.ID {
				t.Errorf("blockedBy should only contain activeBlocker.ID, got: %v", blockedBy)
			}
		}
	}
}
