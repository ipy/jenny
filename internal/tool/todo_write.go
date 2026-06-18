// Package tool provides the tool interface and implementations.
package tool

import (
	"context"
	"sync"
)

// TodoItem represents a single todo item.
type TodoItem struct {
	Subject    string
	ActiveForm string
	Completed  bool
}

// TodoWriteTool provides basic in-session todo tracking.
type TodoWriteTool struct {
	mu    sync.Mutex
	items map[string][]TodoItem
}

// NewTodoWriteTool creates a new TodoWriteTool.
func NewTodoWriteTool() *TodoWriteTool {
	return &TodoWriteTool{
		items: make(map[string][]TodoItem),
	}
}

// Name returns the tool name.
func (t *TodoWriteTool) Name() string {
	return "TodoWrite"
}

// ConcurrencySafe returns false — todo writes must execute serially
// to avoid corrupting the TODO.md file.
func (t *TodoWriteTool) ConcurrencySafe() bool {
	return false
}


// Description returns a description of the tool.
func (t *TodoWriteTool) Description() string {
	return "Manage in-session todo items. Create, update, or complete todos by subject."
}

// InputSchema returns the JSON schema for tool input.
func (t *TodoWriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: create, update, or complete",
				"enum":        []string{"create", "update", "complete"},
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "The subject/topic of the todo item",
			},
			"activeForm": map[string]any{
				"type":        "string",
				"description": "The active form describing the task (e.g., 'Implementing login')",
			},
		},
		"required": []string{"action", "subject"},
	}
}

// Execute runs the TodoWrite tool.
func (t *TodoWriteTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Get action (required)
	action, ok := input["action"].(string)
	if !ok || action == "" {
		return &ToolResult{
			Content: "action is required (create, update, complete)",
			IsError: true,
		}, nil
	}

	// Get subject (required)
	subject, ok := input["subject"].(string)
	if !ok || subject == "" {
		return &ToolResult{
			Content: "subject is required",
			IsError: true,
		}, nil
	}

	// Get activeForm (optional)
	var activeForm string
	if af, ok := input["activeForm"].(string); ok {
		activeForm = af
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	switch action {
	case "create":
		return t.createItem(subject, activeForm)
	case "update":
		return t.updateItem(subject, activeForm)
	case "complete":
		return t.completeItem(subject)
	default:
		return &ToolResult{
			Content: "unknown action: " + action + ". Use create, update, or complete",
			IsError: true,
		}, nil
	}
}

// createItem adds a new todo item to a list (keyed by subject category).
func (t *TodoWriteTool) createItem(subject, activeForm string) (*ToolResult, error) {
	// Use subject as the list key (each subject has its own list)
	list := t.items[subject]
	list = append(list, TodoItem{
		Subject:    subject,
		ActiveForm: activeForm,
		Completed:  false,
	})
	t.items[subject] = list

	return &ToolResult{
		Content: "Created todo: " + activeForm,
		IsError: false,
	}, nil
}

// updateItem updates an existing incomplete todo item's activeForm.
func (t *TodoWriteTool) updateItem(subject, activeForm string) (*ToolResult, error) {
	list, ok := t.items[subject]
	if !ok || len(list) == 0 {
		return &ToolResult{
			Content: "todo not found: " + subject,
			IsError: true,
		}, nil
	}

	// Find first incomplete item and update it
	found := false
	for i, item := range list {
		if !item.Completed {
			list[i].ActiveForm = activeForm
			found = true
			break
		}
	}

	if !found {
		return &ToolResult{
			Content: "no incomplete todo found for: " + subject,
			IsError: true,
		}, nil
	}

	t.items[subject] = list
	return &ToolResult{
		Content: "Updated todo: " + activeForm,
		IsError: false,
	}, nil
}

// completeItem marks the first incomplete todo as completed.
// When all items in a list are completed, the list is cleared.
func (t *TodoWriteTool) completeItem(subject string) (*ToolResult, error) {
	list, ok := t.items[subject]
	if !ok || len(list) == 0 {
		return &ToolResult{
			Content: "todo not found: " + subject,
			IsError: true,
		}, nil
	}

	// Find first incomplete item and mark it complete
	found := false
	for i, item := range list {
		if !item.Completed {
			list[i].Completed = true
			found = true
			break
		}
	}

	if !found {
		return &ToolResult{
			Content: "no incomplete todo found for: " + subject,
			IsError: true,
		}, nil
	}

	// Check if all items are completed - if so, clear the list
	allCompleted := true
	for _, item := range list {
		if !item.Completed {
			allCompleted = false
			break
		}
	}

	if allCompleted {
		delete(t.items, subject)
		return &ToolResult{
			Content: "All todos completed for: " + subject,
			IsError: false,
		}, nil
	}

	t.items[subject] = list
	return &ToolResult{
		Content: "Completed todo for: " + subject,
		IsError: false,
	}, nil
}
