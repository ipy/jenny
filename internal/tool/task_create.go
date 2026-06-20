package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskCreateTool creates tracked tasks. Part of the task tracking system.
type TaskCreateTool struct {
	store *TaskStore
}

// NewTaskCreateTool creates a new TaskCreateTool.
func NewTaskCreateTool(store *TaskStore) *TaskCreateTool {
	return &TaskCreateTool{store: store}
}

// Name returns the tool name.
func (t *TaskCreateTool) Name() string {
	return "TaskCreate"
}

// ConcurrencySafe returns false — task creation should be serialized to
// avoid logical race conditions in the todo system.
func (t *TaskCreateTool) ConcurrencySafe() bool {
	return false
}

// Description returns a description of the tool.
func (t *TaskCreateTool) Description() string {
	return "Creates a tracked task. Use for organizing complex multi-step work."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskCreateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": map[string]any{
				"type":        "string",
				"description": "The subject/title of the task (required)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Detailed description of the task",
			},
			"active_form": map[string]any{
				"type":        "string",
				"description": "The active form describing current work (e.g., 'Implementing login')",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "Additional metadata for the task",
			},
		},
		"required": []string{"subject"},
	}
}

// Execute runs the TaskCreate tool.
func (t *TaskCreateTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Get subject (required)
	subject, ok := input["subject"].(string)
	if !ok || subject == "" {
		return &ToolResult{
			Content: "subject is required",
			IsError: true,
		}, nil
	}

	// Get description (optional, defaults to "")
	description := ""
	if desc, ok := input["description"].(string); ok {
		description = desc
	}

	// Get active_form (optional)
	activeForm := ""
	if af, ok := input["active_form"].(string); ok {
		activeForm = af
	}

	// Get metadata (optional)
	var metadata map[string]any
	if meta, ok := input["metadata"].(map[string]any); ok {
		metadata = meta
	}

	task, err := t.store.Create(subject, description, activeForm, metadata)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("failed to create task: %v", err),
			IsError: true,
		}, nil
	}

	// Serialize task to JSON for output
	data, _ := json.Marshal(map[string]any{
		"id":          task.ID,
		"subject":     task.Subject,
		"description": task.Description,
		"active_form": task.ActiveForm,
		"status":      task.Status,
		"created_at":  task.CreatedAt,
		"metadata":    task.Metadata,
	})

	return &ToolResult{
		Content: string(data),
		IsError: false,
	}, nil
}
