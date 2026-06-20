package tool

import (
	"context"
	"encoding/json"
)

// TaskListTool lists tasks with optional filters.
type TaskListTool struct {
	store *TaskStore
}

// NewTaskListTool creates a new TaskListTool.
func NewTaskListTool(store *TaskStore) *TaskListTool {
	return &TaskListTool{store: store}
}

// Name returns the tool name.
func (t *TaskListTool) Name() string {
	return "TaskList"
}

// Description returns a description of the tool.
func (t *TaskListTool) Description() string {
	return "Lists all tasks with status and dependencies. Use to review pending work and decide what to do next, check which tasks are blocked, find newly unblocked work after completing a task, or restore awareness of task state after context compaction. Prefer working on unblocked tasks in creation order. Use TaskGet for full details including acceptance criteria and constraints."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskListTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "Optional status filter: pending, in_progress, completed, or deleted",
			},
		},
	}
}

// Execute runs the TaskList tool.
func (t *TaskListTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	var filter TaskFilter
	if status, ok := input["status"].(string); ok && status != "" {
		filter.Status = TaskStatus(status)
	}

	tasks := t.store.List(filter)

	result := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		out := map[string]any{
			"id":      task.ID,
			"subject": task.Subject,
			"status":  task.Status,
		}

		// Only include blocked_by if there are active blockers
		if len(task.BlockedBy) > 0 {
			filteredBlockers := make([]string, 0)
			for _, blockerID := range task.BlockedBy {
				blocker := t.store.Get(blockerID)
				if blocker != nil && blocker.Status != TaskStatusCompleted && blocker.Status != TaskStatusDeleted {
					filteredBlockers = append(filteredBlockers, blockerID)
				}
			}
			if len(filteredBlockers) > 0 {
				out["blocked_by"] = filteredBlockers
			}
		}

		result = append(result, out)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return &ToolResult{
			Content: "failed to serialize tasks",
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: string(data),
		IsError: false,
	}, nil
}
