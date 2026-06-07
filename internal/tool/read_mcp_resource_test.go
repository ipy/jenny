package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/mcp"
)

// TestReadMcpResourceTool_NameAndDescription tests basic tool metadata.
func TestReadMcpResourceTool_NameAndDescription(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	if readTool.Name() != "read_mcp_resource" {
		t.Errorf("expected name 'read_mcp_resource', got %q", readTool.Name())
	}

	desc := readTool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	schema := readTool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

// TestReadMcpResourceTool_AC1_UnknownServer tests AC1: unknown server returns error with available names.
func TestReadMcpResourceTool_AC1_UnknownServer(t *testing.T) {
	// Note: We can't easily reset the global mcp clients map from the tool package,
	// so we just test with an intentionally unknown server name
	readTool := NewReadMcpResourceTool()

	result, err := readTool.Execute(context.Background(), map[string]any{
		"server": "definitely-nonexistent-server-12345",
		"uri":    "file:///test.txt",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for invalid server")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content")
	}
	// Should mention the invalid server name
	if !strings.Contains(result.Content, "definitely-nonexistent-server-12345") {
		t.Errorf("error should mention server name, got: %s", result.Content)
	}
	// Should list available servers
	if !strings.Contains(result.Content, "Available servers") {
		t.Errorf("error should list available servers, got: %s", result.Content)
	}
}

// TestReadMcpResourceTool_MissingServer tests error when server parameter is missing.
func TestReadMcpResourceTool_MissingServer(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	result, err := readTool.Execute(context.Background(), map[string]any{
		"uri": "file:///test.txt",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing server")
	}
	if !strings.Contains(result.Content, "server parameter is required") {
		t.Errorf("expected 'server parameter is required' error, got: %s", result.Content)
	}
}

// TestReadMcpResourceTool_MissingURI tests error when uri parameter is missing.
func TestReadMcpResourceTool_MissingURI(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	result, err := readTool.Execute(context.Background(), map[string]any{
		"server": "test-server",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing uri")
	}
	if !strings.Contains(result.Content, "uri parameter is required") {
		t.Errorf("expected 'uri parameter is required' error, got: %s", result.Content)
	}
}

// TestReadMcpResourceTool_ExecuteInterface tests that the tool implements the tool.Tool interface.
func TestReadMcpResourceTool_ExecuteInterface(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	// Verify it can be called with the correct signature
	result, err := readTool.Execute(context.Background(), map[string]any{
		"server": "any-server",
		"uri":    "file:///test.txt",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
	// Result should be an error since server doesn't exist
	if !result.IsError {
		t.Error("expected error for non-existent server")
	}
}

// TestReadMcpResourceTool_ImplementsToolInterface verifies ReadMcpResourceTool implements Tool interface.
func TestReadMcpResourceTool_ImplementsToolInterface(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	// Compile-time check: verify ReadMcpResourceTool implements Tool
	var _ Tool = readTool
}

// TestReadMcpResourceTool_InputSchemaRequiredFields verifies required fields in schema.
func TestReadMcpResourceTool_InputSchemaRequiredFields(t *testing.T) {
	readTool := NewReadMcpResourceTool()
	schema := readTool.InputSchema()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	serverProp, ok := props["server"]
	if !ok {
		t.Fatal("expected server property")
	}
	serverSchema := serverProp.(map[string]any)
	if serverSchema["type"] != "string" {
		t.Errorf("expected server type string, got %v", serverSchema["type"])
	}

	uriProp, ok := props["uri"]
	if !ok {
		t.Fatal("expected uri property")
	}
	uriSchema := uriProp.(map[string]any)
	if uriSchema["type"] != "string" {
		t.Errorf("expected uri type string, got %v", uriSchema["type"])
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required field")
	}
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(required))
	}
}

// Note: AC2-AC5 require integration testing with a real MCP client/server.
// These acceptance criteria are tested via:
// - mcp/read_resource_test.go: tests ReadResource method on Client directly
// - Integration tests that exercise the full MCP flow

var _ = mcp.GetClient // Reference mcp package to ensure it compiles
