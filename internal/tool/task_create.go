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
	return "Creates a tracked task for organizing complex multi-step work. Use for tasks that require 3 or more distinct steps, non-trivial work, or when the user provides multiple tasks. After creating tasks, use TaskList to review and TaskUpdate to begin work."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskCreateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": map[string]any{
				"type":        "string",
				"description": "A brief title for the task (required)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "What needs to be done",
			},
			"acceptance_criteria": map[string]any{
				"type":        "string",
				"description": "Verifiable conditions that must hold for the task to be considered done",
			},
			"constraints": map[string]any{
				"type":        "string",
				"description": "Implicit requirements or restrictions (compatibility, conventions, dependencies)",
			},
			"metadata": map[string]any{
				"type":        "object",
				"description": "Arbitrary metadata to attach to the task",
			},
		},
		"required": []string{"subject"},
	}
}

// Execute runs the TaskCreate tool.
func (t *TaskCreateTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	subject, ok := input["subject"].(string)
	if !ok || subject == "" {
		return &ToolResult{
			Content: "subject is required",
			IsError: true,
		}, nil
	}

	description := ""
	if desc, ok := input["description"].(string); ok {
		description = desc
	}

	acceptanceCriteria := ""
	if ac, ok := input["acceptance_criteria"].(string); ok {
		acceptanceCriteria = ac
	}

	constraints := ""
	if c, ok := input["constraints"].(string); ok {
		constraints = c
	}

	var metadata map[string]any
	if meta, ok := input["metadata"].(map[string]any); ok {
		metadata = meta
	}

	task, err := t.store.Create(subject, description, acceptanceCriteria, constraints, metadata)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("failed to create task: %v", err),
			IsError: true,
		}, nil
	}

	data, _ := json.Marshal(map[string]any{
		"id":                  task.ID,
		"subject":             task.Subject,
		"description":         task.Description,
		"acceptance_criteria": task.AcceptanceCriteria,
		"constraints":         task.Constraints,
		"status":              task.Status,
		"created_at":          task.CreatedAt,
		"metadata":            task.Metadata,
	})

	return &ToolResult{
		Content: string(data),
		IsError: false,
	}, nil
}
