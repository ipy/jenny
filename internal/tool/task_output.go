// Package tool provides the tool interface and implementations.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TaskOutputTool retrieves output from background tasks.
type TaskOutputTool struct {
	taskManager *TaskManager
}

// NewTaskOutputTool creates a new TaskOutputTool with the given task manager.
func NewTaskOutputTool(tm *TaskManager) *TaskOutputTool {
	return &TaskOutputTool{taskManager: tm}
}

// Name returns the tool name.
func (t *TaskOutputTool) Name() string {
	return "TaskOutput"
}

// Description returns a description of the tool.
func (t *TaskOutputTool) Description() string {
	return "Retrieve output from a background task by its task_id. Supports blocking and non-blocking modes."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskOutputTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The ID of the background task",
			},
			"block": map[string]any{
				"type":        "boolean",
				"description": "If true, wait for task completion (default: true)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Maximum seconds to wait for completion (default: 30, max: 600)",
			},
		},
		"required": []string{"task_id"},
	}
}

// Execute retrieves output from a background task.
func (t *TaskOutputTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Get task_id (required)
	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return &ToolResult{
			Content: "task_id is required",
			IsError: true,
		}, nil
	}

	// Get block parameter (default: true)
	block := true
	if b, ok := input["block"].(bool); ok {
		block = b
	}

	// Get timeout parameter (default: 30s, max: 600s)
	timeoutSeconds := 30
	if t, ok := input["timeout"].(float64); ok {
		timeoutSeconds = min(int(t), 600)
	}

	if t.taskManager == nil {
		return &ToolResult{
			Content: "task manager not available",
			IsError: true,
		}, nil
	}

	// Try to get output from completion queue first (in-memory result)
	completions := t.taskManager.DrainCompletions()
	for _, c := range completions {
		if c.TaskID == taskID {
			return &ToolResult{
				Content: c.Output,
				IsError: false,
			}, nil
		}
	}

	// Check if task exists
	info, found := t.taskManager.Load(taskID)
	if !found {
		return &ToolResult{
			Content: "task not found",
			IsError: false,
		}, nil
	}

	// If not blocking, return current state
	if !block {
		return &ToolResult{
			Content: fmt.Sprintf(`{"task_id": "%s", "state": "%s"}`, taskID, info.State),
			IsError: false,
		}, nil
	}

	// Blocking: wait for completion or timeout
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &ToolResult{
				Content: "task output retrieval cancelled",
				IsError: true,
			}, nil
		case <-ticker.C:
			// Check completion queue first (early exit)
			completions := t.taskManager.DrainCompletions()
			for _, c := range completions {
				if c.TaskID == taskID {
					return &ToolResult{
						Content: c.Output,
						IsError: false,
					}, nil
				}
			}

			// Check task state
			info, found := t.taskManager.Load(taskID)
			if !found {
				return &ToolResult{
					Content: "task not found",
					IsError: false,
				}, nil
			}

			if info.State == TaskStateCompleted || info.State == TaskStateStopped {
				// Read output from file
				output, err := t.readTaskOutput(taskID)
				if err != nil {
					return &ToolResult{
						Content: fmt.Sprintf("task ended but failed to read output: %v", err),
						IsError: true,
					}, nil
				}
				return &ToolResult{
					Content: output,
					IsError: false,
				}, nil
			}

			// Check timeout
			if time.Now().After(deadline) {
				return &ToolResult{
					Content: fmt.Sprintf("timeout waiting for task %s", taskID),
					IsError: true,
				}, nil
			}
		}
	}
}

// readTaskOutput reads and parses the task output file.
func (t *TaskOutputTool) readTaskOutput(taskID string) (string, error) {
	path, err := t.taskManager.TaskOutputPath(taskID)
	if err != nil {
		return "", fmt.Errorf("getting output path: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("output file not found")
		}
		return "", fmt.Errorf("reading output file: %w", err)
	}

	// Parse JSONL entry (last line contains the final result)
	lines := splitJSONLLines(data)
	if len(lines) == 0 {
		return "", fmt.Errorf("empty output file")
	}

	// Parse the last entry (final result)
	var entry TaskResultEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		return "", fmt.Errorf("parsing output: %w", err)
	}

	return entry.Output, nil
}

// splitJSONLLines splits JSONL data into lines, handling the last line without newline.
func splitJSONLLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if line := string(data[start:i]); line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	// Add remaining content as last line
	if remaining := string(data[start:]); remaining != "" {
		lines = append(lines, remaining)
	}
	return lines
}
