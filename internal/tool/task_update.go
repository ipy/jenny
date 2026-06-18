package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"
)

// TaskUpdateTool updates a task's fields, metadata, and dependencies.
type TaskUpdateTool struct {
	store *TaskStore
}

// NewTaskUpdateTool creates a new TaskUpdateTool.
func NewTaskUpdateTool(store *TaskStore) *TaskUpdateTool {
	return &TaskUpdateTool{store: store}
}

// Name returns the tool name.
func (t *TaskUpdateTool) Name() string {
	return "TaskUpdate"
}

// ConcurrencySafe returns false — task updates should be serialized to
// avoid logical race conditions in the todo system.
func (t *TaskUpdateTool) ConcurrencySafe() bool {
	return false
}

// Description returns a description of the tool.
func (t *TaskUpdateTool) Description() string {
	return "Updates a task in the Todo v2 system. Supports field updates, metadata merge (null deletes keys), and dependency graph changes."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskUpdateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The ID of the task to update (required)",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "The new subject/title of the task",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "The new description of the task",
			},
			"active_form": map[string]any{
				"type":        "string",
				"description": "The new active form describing current work",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "The new status: pending, in_progress, completed, or deleted",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "Metadata to merge. Null values delete keys from existing metadata.",
			},
			"add_blocks": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Task IDs that this task blocks",
			},
			"add_blocked_by": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Task IDs that this task is blocked by",
			},
		},
		"required": []string{"task_id"},
	}
}

// Execute runs the TaskUpdate tool.
func (t *TaskUpdateTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return &ToolResult{
			Content: "task_id is required",
			IsError: true,
		}, nil
	}

	// Check task exists
	task := t.store.Get(taskID)
	if task == nil {
		return &ToolResult{
			Content: "task not found",
			IsError: true,
		}, nil
	}

	// Build fields map for update
	fields := make(map[string]any)

	if subject, ok := input["subject"].(string); ok {
		fields["subject"] = subject
	}
	if description, ok := input["description"].(string); ok {
		fields["description"] = description
	}
	if activeForm, ok := input["active_form"].(string); ok {
		fields["active_form"] = activeForm
	}
	if status, ok := input["status"].(string); ok {
		if !isValidStatus(status) {
			return &ToolResult{
				Content: fmt.Sprintf("invalid status: %s (must be pending, in_progress, completed, or deleted)", status),
				IsError: true,
			}, nil
		}
		fields["status"] = status
	}

	// Metadata merge: null values delete keys
	if metadata, ok := input["metadata"].(map[string]any); ok {
		merged := make(map[string]any)
		// Copy existing metadata
		maps.Copy(merged, task.Metadata)
		// Apply new metadata (null deletes)
		for k, v := range metadata {
			if v == nil {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		fields["metadata"] = merged
	}

	// Perform update
	updated := t.store.Update(taskID, fields)
	if updated == nil {
		return &ToolResult{
			Content: "failed to update task",
			IsError: true,
		}, nil
	}

	// Handle dependency updates
	var addBlocks, addBlockedBy []string
	if blocks, ok := input["add_blocks"].([]any); ok {
		for _, b := range blocks {
			if s, ok := b.(string); ok {
				addBlocks = append(addBlocks, s)
			}
		}
	}
	if blockedBy, ok := input["add_blocked_by"].([]any); ok {
		for _, b := range blockedBy {
			if s, ok := b.(string); ok {
				addBlockedBy = append(addBlockedBy, s)
			}
		}
	}
	if len(addBlocks) > 0 || len(addBlockedBy) > 0 {
		updated = t.store.AddDependencies(taskID, addBlocks, addBlockedBy)
	}

	// Serialize response
	data, err := json.Marshal(map[string]any{
		"id":          updated.ID,
		"subject":     updated.Subject,
		"description": updated.Description,
		"active_form": updated.ActiveForm,
		"status":      updated.Status,
		"created_at":  updated.CreatedAt.Format(time.RFC3339),
		"updated_at":  updated.UpdatedAt.Format(time.RFC3339),
		"metadata":    updated.Metadata,
		"blocks":      updated.Blocks,
		"blocked_by":  updated.BlockedBy,
	})
	if err != nil {
		return &ToolResult{
			Content: "failed to serialize task",
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: string(data),
		IsError: false,
	}, nil
}

func isValidStatus(s string) bool {
	switch TaskStatus(s) {
	case TaskStatusPending, TaskStatusInProgress, TaskStatusCompleted, TaskStatusDeleted:
		return true
	default:
		return false
	}
}
