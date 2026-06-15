// Package tool provides the tool interface and implementations for the agent.
package tool

import (
	"context"

	"github.com/ipy/jenny/internal/toolresult"
)

// ToolResult represents the result of a tool execution.
type ToolResult = toolresult.ToolResult

// Tool defines the interface for agent tools.
type Tool interface {
	// Name returns the tool's name.
	Name() string
	// Description returns a description of the tool for the model.
	Description() string
	// InputSchema returns the JSON schema for tool input.
	InputSchema() map[string]any
	// Execute runs the tool with the given input and returns the result.
	// If the context is cancelled, the tool should abort any in-progress work.
	Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error)
}

// ConcurrentTool is an optional interface that tools can implement to declare
// whether they are safe to run concurrently with other tools.
// Tools that don't implement this are assumed to be concurrency-safe unless
// the executor applies name-based overrides (e.g., Bash is always serial).
type ConcurrentTool interface {
	ConcurrencySafe() bool
}

// IsConcurrencySafe returns whether a tool declares itself safe for concurrent execution.
// Returns true (safe) if the tool does not implement ConcurrentTool.
func IsConcurrencySafe(t Tool) bool {
	if ct, ok := t.(ConcurrentTool); ok {
		return ct.ConcurrencySafe()
	}
	return true
}

// ToolUse represents a tool use request from the model.
type ToolUse struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"input"`
}

// FindTool finds a tool by name from a list of tools.
func FindTool(tools []Tool, name string) Tool {
	for _, t := range tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}
