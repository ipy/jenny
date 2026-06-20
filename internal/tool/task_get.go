package tool

import (
	"context"
	"encoding/json"
)

// TaskGetTool retrieves a single task by ID.
type TaskGetTool struct {
	store *TaskStore
}

// NewTaskGetTool creates a new TaskGetTool.
func NewTaskGetTool(store *TaskStore) *TaskGetTool {
	return &TaskGetTool{store: store}
}

// Name returns the tool name.
func (t *TaskGetTool) Name() string {
	return "TaskGet"
}

// Description returns a description of the tool.
func (t *TaskGetTool) Description() string {
	return "Retrieves a single task by ID with full details including description, acceptance criteria, and constraints. Use before starting work on a task to review requirements, or after TaskList to inspect a specific task. Returns 'task not found' if the task does not exist."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskGetTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The ID of the task to retrieve (required)",
			},
		},
		"required": []string{"task_id"},
	}
}

// Execute runs the TaskGet tool.
func (t *TaskGetTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return &ToolResult{
			Content: "task_id is required",
			IsError: true,
		}, nil
	}

	task := t.store.Get(taskID)
	if task == nil {
		return &ToolResult{
			Content: "task not found",
			IsError: false,
		}, nil
	}

	data, err := json.Marshal(task)
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
