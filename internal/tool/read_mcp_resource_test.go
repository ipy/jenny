package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// TestReadMcpResourceTool_AC2_TextInline tests AC2: text content is returned inline.
func TestReadMcpResourceTool_AC2_TextInline(t *testing.T) {
	// Save and restore global state
	mcp.ResetTestClients()
	mcp.ResetReadResourceHook()
	t.Cleanup(func() {
		mcp.ResetTestClients()
		mcp.ResetReadResourceHook()
	})

	// Register mock client
	mcp.SetTestClient("test-server", &mcp.Client{Name: "test-server"})

	// Set up hook to return text content
	mcp.SetReadResourceHook(func(ctx context.Context, clientName string, uri string) ([]mcp.ResourceContent, error) {
		if clientName == "test-server" && uri == "file:///test.txt" {
			return []mcp.ResourceContent{
				{Type: "text", Text: "Hello, World!", MimeType: "text/plain"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected client/uri: %s/%s", clientName, uri)
	})

	tool := NewReadMcpResourceTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "test-server",
		"uri":    "file:///test.txt",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Parse output JSON
	var output struct {
		Contents []struct {
			URI         string `json:"uri"`
			MimeType    string `json:"mimeType,omitempty"`
			Text        string `json:"text,omitempty"`
			BlobSavedTo string `json:"blobSavedTo,omitempty"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	if len(output.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(output.Contents))
	}
	if output.Contents[0].Text != "Hello, World!" {
		t.Errorf("expected text 'Hello, World!', got %q", output.Contents[0].Text)
	}
	if output.Contents[0].BlobSavedTo != "" {
		t.Errorf("expected no blobSavedTo for text content, got %s", output.Contents[0].BlobSavedTo)
	}
	if output.Contents[0].MimeType != "text/plain" {
		t.Errorf("expected mimeType 'text/plain', got %q", output.Contents[0].MimeType)
	}
}

// TestReadMcpResourceTool_AC3_BlobPersist tests AC3: binary content is decoded and saved to disk.
func TestReadMcpResourceTool_AC3_BlobPersist(t *testing.T) {
	// Save and restore global state
	mcp.ResetTestClients()
	mcp.ResetReadResourceHook()
	t.Cleanup(func() {
		mcp.ResetTestClients()
		mcp.ResetReadResourceHook()
	})

	// Register mock client
	mcp.SetTestClient("blob-server", &mcp.Client{Name: "blob-server"})

	// Set up hook to return blob content
	mcp.SetReadResourceHook(func(ctx context.Context, clientName string, uri string) ([]mcp.ResourceContent, error) {
		if clientName == "blob-server" && uri == "file:///image.png" {
			return []mcp.ResourceContent{
				{Type: "blob", Blob: []byte("Hello"), MimeType: "image/png"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected client/uri: %s/%s", clientName, uri)
	})

	cwd := t.TempDir()
	tool := NewReadMcpResourceTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "blob-server",
		"uri":    "file:///image.png",
	}, cwd)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Parse output JSON
	var output struct {
		Contents []struct {
			URI         string `json:"uri"`
			MimeType    string `json:"mimeType,omitempty"`
			BlobSavedTo string `json:"blobSavedTo,omitempty"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	if len(output.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(output.Contents))
	}
	if output.Contents[0].BlobSavedTo == "" {
		t.Fatal("expected blobSavedTo to be set for blob content")
	}

	// Verify file exists and contains decoded data
	data, err := os.ReadFile(output.Contents[0].BlobSavedTo)
	if err != nil {
		t.Fatalf("failed to read saved blob file: %v", err)
	}
	if string(data) != "Hello" {
		t.Errorf("expected file content 'Hello', got %q", string(data))
	}

	// Verify file is in the correct directory
	expectedDir := filepath.Join(cwd, ".jenny", "mcp-resources")
	if filepath.Dir(output.Contents[0].BlobSavedTo) != expectedDir {
		t.Errorf("expected file in %s, got %s", expectedDir, filepath.Dir(output.Contents[0].BlobSavedTo))
	}
}

// TestReadMcpResourceTool_AC4_PersistFailure tests AC4: disk write failure returns error, not base64.
func TestReadMcpResourceTool_AC4_PersistFailure(t *testing.T) {
	// Save and restore global state
	mcp.ResetTestClients()
	mcp.ResetReadResourceHook()
	t.Cleanup(func() {
		mcp.ResetTestClients()
		mcp.ResetReadResourceHook()
	})

	// Register mock client
	mcp.SetTestClient("fail-server", &mcp.Client{Name: "fail-server"})

	// Set up hook to return blob content
	mcp.SetReadResourceHook(func(ctx context.Context, clientName string, uri string) ([]mcp.ResourceContent, error) {
		if clientName == "fail-server" {
			return []mcp.ResourceContent{
				{Type: "blob", Blob: []byte("Hello"), MimeType: "image/png"},
			}, nil
		}
		return nil, fmt.Errorf("unexpected client/uri: %s/%s", clientName, uri)
	})

	// Use a path that cannot be written to (empty string or invalid path)
	cwd := "/nonexistent/path/that/cannot/be/created"
	tool := NewReadMcpResourceTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "fail-server",
		"uri":    "file:///image.png",
	}, cwd)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for persist failure")
	}
	if !strings.Contains(result.Content, "Error saving binary content to disk") {
		t.Errorf("expected persist error message, got: %s", result.Content)
	}
	// AC4: The result must NOT contain the raw base64 data
	if strings.Contains(result.Content, "SGVsbG8=") {
		t.Error("result must NOT contain raw base64 data when persist fails")
	}
}

// TestReadMcpResourceTool_AC5_ConcurrentCalls tests AC5: concurrent calls are safe.
func TestReadMcpResourceTool_AC5_ConcurrentCalls(t *testing.T) {
	// Save and restore global state
	mcp.ResetTestClients()
	t.Cleanup(func() {
		mcp.ResetTestClients()
		mcp.ResetReadResourceHook()
	})

	// Register mock client
	mcp.SetTestClient("concurrent-server", &mcp.Client{Name: "concurrent-server"})

	counter := atomic.Int64{}
	mcp.SetReadResourceHook(func(ctx context.Context, clientName string, uri string) ([]mcp.ResourceContent, error) {
		counter.Add(1)
		return []mcp.ResourceContent{
			{Type: "text", Text: "response", MimeType: "text/plain"},
		}, nil
	})

	cwd := t.TempDir()
	tool := NewReadMcpResourceTool()

	var wg sync.WaitGroup
	const numGoroutines = 10
	for range numGoroutines {
		wg.Go(func() {
			result, err := tool.Execute(context.Background(), map[string]any{
				"server": "concurrent-server",
				"uri":    "file:///test.txt",
			}, cwd)
			if err != nil {
				t.Errorf("Execute returned error: %v", err)
				return
			}
			if result.IsError {
				t.Errorf("unexpected error: %s", result.Content)
			}
		})
	}
	wg.Wait()

	if counter.Load() != numGoroutines {
		t.Errorf("expected %d calls, got %d", numGoroutines, counter.Load())
	}
}

var _ = mcp.GetClient // Reference mcp package to ensure it compiles
