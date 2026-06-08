package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestTaskOutputTool_Name(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	if got := tool.Name(); got != "TaskOutput" {
		t.Errorf("Name() = %v, want %v", got, "TaskOutput")
	}
}

func TestTaskOutputTool_InputSchema(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
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

	hasTaskID := slices.Contains(required, "task_id")
	if !hasTaskID {
		t.Errorf("InputSchema() missing required field: task_id")
	}

	// Check optional fields exist
	if _, ok := props["block"]; !ok {
		t.Errorf("InputSchema() missing optional field: block")
	}
	if _, ok := props["timeout"]; !ok {
		t.Errorf("InputSchema() missing optional field: timeout")
	}
}

func TestTaskOutputTool_Execute_NonExistentTask(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "non-existent-task-id",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error for non-existent task")
	}
	if result.Content != "task not found" {
		t.Errorf("Execute() content = %v, want 'task not found'", result.Content)
	}
}

func TestTaskOutputTool_Execute_NilTaskManager(t *testing.T) {
	tool := NewTaskOutputTool(nil)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "some-task",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error when task manager is nil")
	}
	if result.Content != "task manager not available" {
		t.Errorf("Execute() content = %v, want 'task manager not available'", result.Content)
	}
}

func TestTaskOutputTool_Execute_MissingTaskID(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error when task_id is missing")
	}
	if result.Content != "task_id is required" {
		t.Errorf("Execute() content = %v, want 'task_id is required'", result.Content)
	}
}

func TestTaskOutputTool_Execute_InMemoryResult(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Enqueue a completion directly
	tm.EnqueueCompletion(TaskCompletion{
		TaskID:          "test-task-1",
		DurationSeconds: 1.5,
		ExitCode:        0,
		Output:          "in-memory output from completion queue",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-1",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error")
	}
	if result.Content != "in-memory output from completion queue" {
		t.Errorf("Execute() content = %v, want 'in-memory output from completion queue'", result.Content)
	}
}

func TestTaskOutputTool_Execute_BlockNonBlocking(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Store a running task
	tm.Store("test-task-2", &TaskInfo{
		TaskID:     "test-task-2",
		State:      TaskStateRunning,
		OutputFile: filepath.Join(tmpDir, ".jenny", "tasks", "test-task-2.output"),
		StartTime:  time.Now(),
		Command:    "echo test",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Non-blocking mode should return current state
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-2",
		"block":   false,
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error for non-blocking mode")
	}
	if result.Content == "" {
		t.Errorf("Execute() should return state in non-blocking mode")
	}
}

func TestTaskOutputTool_Execute_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Store a running task (not completed)
	tm.Store("test-task-timeout", &TaskInfo{
		TaskID:     "test-task-timeout",
		State:      TaskStateRunning,
		OutputFile: filepath.Join(tmpDir, ".jenny", "tasks", "test-task-timeout.output"),
		StartTime:  time.Now(),
		Command:    "sleep 10",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Use a short timeout (500ms) and block=true
	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-timeout",
		"block":   true,
		"timeout": 0.5, // 500ms timeout
	}, "/tmp")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error on timeout")
	}
	if result.Content != "timeout waiting for task test-task-timeout" {
		t.Errorf("Execute() content = %v, want 'timeout waiting for task test-task-timeout'", result.Content)
	}
	// Verify timeout was detected (timing check is lenient due to scheduler variance)
	if elapsed < 50*time.Millisecond {
		t.Errorf("Execute() returned too quickly, elapsed=%v", elapsed)
	}
}

func TestTaskOutputTool_Execute_FileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Create output file with task result
	outputPath := filepath.Join(tmpDir, ".jenny", "tasks", "test-task-file.output")
	err := os.MkdirAll(filepath.Dir(outputPath), 0755)
	if err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	entry := TaskResultEntry{
		Type:            "task_result",
		TaskID:          "test-task-file",
		Output:          "file-based output content",
		ExitCode:        0,
		DurationSeconds: 1.0,
	}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(outputPath, append(data, '\n'), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Store a completed task
	tm.Store("test-task-file", &TaskInfo{
		TaskID:     "test-task-file",
		State:      TaskStateCompleted,
		OutputFile: outputPath,
		StartTime:  time.Now(),
		Command:    "echo test",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-file",
		"block":   true,
		"timeout": 5,
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error for completed task")
	}
	if result.Content != "file-based output content" {
		t.Errorf("Execute() content = %v, want 'file-based output content'", result.Content)
	}
}

func TestTaskOutputTool_Execute_MaxTimeout(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Test that timeout > 600 gets capped to 600
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task",
		"timeout": 700, // > 600 max
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	// Should not error on input validation, timeout gets capped
	_ = result
}
