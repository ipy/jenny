package tool

import (
	"context"
	"encoding/json"
	"time"
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
	return "Lists tasks from the task tracking system. Optionally filters by status. Internal metadata keys (prefixed with _) are stripped from output."
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

	// Convert tasks to output format, filtering internal metadata
	// and stripping resolved blockers from blockedBy arrays
	result := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		// Build output task map
		out := map[string]any{
			"id":          task.ID,
			"subject":     task.Subject,
			"description": task.Description,
			"active_form": task.ActiveForm,
			"status":      task.Status,
			"created_at":  task.CreatedAt.Format(time.RFC3339),
			"updated_at":  task.UpdatedAt.Format(time.RFC3339),
			"blocks":      task.Blocks,
		}

		// Filter internal metadata keys
		if len(task.Metadata) > 0 {
			filteredMeta := make(map[string]any)
			for k, v := range task.Metadata {
				if len(k) > 0 && k[0] == '_' {
					continue // skip _internal keys
				}
				filteredMeta[k] = v
			}
			out["metadata"] = filteredMeta
		} else {
			out["metadata"] = task.Metadata
		}

		// Filter resolved blockers from blockedBy
		if len(task.BlockedBy) > 0 {
			filteredBlockers := make([]string, 0)
			for _, blockerID := range task.BlockedBy {
				blocker := t.store.Get(blockerID)
				if blocker != nil && blocker.Status != TaskStatusCompleted && blocker.Status != TaskStatusDeleted {
					filteredBlockers = append(filteredBlockers, blockerID)
				}
			}
			out["blocked_by"] = filteredBlockers
		} else {
			out["blocked_by"] = []string{}
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
