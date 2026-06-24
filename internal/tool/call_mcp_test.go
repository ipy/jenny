package tool

import (
	"context"
	"testing"
)

func TestCallMcpTool_Name(t *testing.T) {
	tool := NewCallMcpTool()
	if tool.Name() != "call_mcp" {
		t.Errorf("expected name 'call_mcp', got %q", tool.Name())
	}
}

func TestCallMcpTool_InputSchema(t *testing.T) {
	tool := NewCallMcpTool()
	schema := tool.InputSchema()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}

	if _, ok := props["server"]; !ok {
		t.Error("expected 'server' property in schema")
	}
	if _, ok := props["tool"]; !ok {
		t.Error("expected 'tool' property in schema")
	}
	if _, ok := props["arguments"]; !ok {
		t.Error("expected 'arguments' property in schema")
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required array in schema")
	}
	requiredSet := make(map[string]bool)
	for _, r := range required {
		requiredSet[r] = true
	}
	if !requiredSet["server"] {
		t.Error("'server' should be required")
	}
	if !requiredSet["tool"] {
		t.Error("'tool' should be required")
	}
}

func TestCallMcpTool_MissingServer(t *testing.T) {
	tool := NewCallMcpTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"tool": "some_tool",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when server is missing")
	}
}

func TestCallMcpTool_MissingTool(t *testing.T) {
	tool := NewCallMcpTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "some_server",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when tool is missing")
	}
}

func TestCallMcpTool_UnknownServer(t *testing.T) {
	tool := NewCallMcpTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "nonexistent_server",
		"tool":   "some_tool",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for unknown server")
	}
}

func TestRegistry_DynamicMCP_CallMcpRegistered(t *testing.T) {
	mcpTools := []Tool{
		&mockTool{name: "mcp__server__tool1"},
		&mockTool{name: "mcp__server__tool2"},
	}

	tools := NewRegistry().
		WithBaseTools().
		WithHasMCPServers(true).
		WithMCPTools(mcpTools).
		WithDynamicMCP(true).
		Build()

	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}

	if !names["call_mcp"] {
		t.Error("expected 'call_mcp' tool when dynamic MCP is enabled")
	}

	if names["mcp__server__tool1"] {
		t.Error("individual MCP tools should NOT be registered in dynamic MCP mode")
	}
	if names["mcp__server__tool2"] {
		t.Error("individual MCP tools should NOT be registered in dynamic MCP mode")
	}
}

func TestRegistry_StaticMCP_NoCallMcp(t *testing.T) {
	mcpTools := []Tool{
		&mockTool{name: "mcp__server__tool1"},
	}

	tools := NewRegistry().
		WithBaseTools().
		WithHasMCPServers(true).
		WithMCPTools(mcpTools).
		Build()

	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}

	if names["call_mcp"] {
		t.Error("'call_mcp' should NOT be registered in static (default) MCP mode")
	}
	if !names["mcp__server__tool1"] {
		t.Error("individual MCP tools should be registered in static mode")
	}
}

func TestRegistry_DynamicMCP_ResourceToolsStillRegistered(t *testing.T) {
	tools := NewRegistry().
		WithBaseTools().
		WithHasMCPServers(true).
		WithDynamicMCP(true).
		Build()

	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}

	if !names["read_mcp_resource"] {
		t.Error("read_mcp_resource should still be registered in dynamic MCP mode")
	}
}
