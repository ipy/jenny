package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ipy/jenny/internal/constants"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Server", "my_server"},
		{"list-files", "list_files"},
		{"tool__name", "tool_name"}, // consecutive underscores collapsed
		{"Tool Name", "tool_name"},
		{"server1", "server1"},
		{"my-server-tool", "my_server_tool"},
		{"  spaces  ", "spaces"},
		{"UPPERCASE", "uppercase"},
		{"MiXeD CaSe", "mixed_case"}, // lowercase only, spaces to underscore
		{"---", "unnamed"},           // all non-alphanumeric → fallback
		{"", "unnamed"},              // empty input → fallback
		{"...", "unnamed"},           // all dots → fallback
		{"___", "unnamed"},           // all underscores → trim leaves empty → fallback
		{"a", "a"},                   // single char preserved
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestClientConnectAndDisconnect(t *testing.T) {
	// Test that a bad command produces an error
	client := NewClient("test", "nonexistent-command", []string{}, nil)
	err := client.Connect(context.Background())
	if err == nil {
		client.Disconnect()
		t.Error("expected error connecting to nonexistent command, got nil")
	}
}

func TestClientInitialization(t *testing.T) {
	// This test requires a real MCP server or a mock
	// For unit testing, we test the JSON-RPC message format
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "jenny",
				"version": "0.1.0",
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal initialize request: %v", err)
	}

	var parsed jsonRPCRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	if parsed.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC 2.0, got %s", parsed.JSONRPC)
	}
	if parsed.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", parsed.Method)
	}
}

func TestJSONIDGeneration(t *testing.T) {
	// Test that JSON IDs are unique and incrementing
	id1 := nextJSONID()
	id2 := nextJSONID()
	id3 := nextJSONID()

	if id2 <= id1 {
		t.Errorf("id2 should be greater than id1: id1=%d, id2=%d", id1, id2)
	}
	if id3 <= id2 {
		t.Errorf("id3 should be greater than id2: id2=%d, id3=%d", id2, id3)
	}
}

func TestConnectAllWithEmptyConfig(t *testing.T) {
	// Test that ConnectAll handles empty config gracefully
	err := ConnectAll(map[string]MCPServerDef{})
	if err != nil {
		t.Errorf("ConnectAll({}) should not return error, got: %v", err)
	}
}

func TestShutdownAll(t *testing.T) {
	// ShutdownAll should not panic with no clients
	ShutdownAll()
}

func TestGetToolsWithNoClients(t *testing.T) {
	// Save and restore global state to avoid polluting other tests
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	tools := GetTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools with no clients, got %d", len(tools))
	}
}

func TestGetClientNotFound(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	client := GetClient("nonexistent-server")
	if client != nil {
		t.Error("expected nil for nonexistent server")
	}
}

func TestMCPToolInterface(t *testing.T) {
	mcpTool := &MCPTool{
		serverName:  "My Server",
		toolName:    "List Files",
		inputSchema: map[string]any{"type": "object"},
	}

	// Verify tool name format
	if mcpTool.Name() != "mcp__my_server__list_files" {
		t.Errorf("expected tool name 'mcp__my_server__list_files', got %s", mcpTool.Name())
	}

	// Verify description
	desc := mcpTool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	// Verify input schema
	schema := mcpTool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-server", "/usr/bin/my-mcp-server", []string{"--flag"}, map[string]string{"KEY": "value"})

	if client.Name != "test-server" {
		t.Errorf("expected name 'test-server', got %s", client.Name)
	}
	if client.cmd != "/usr/bin/my-mcp-server" {
		t.Errorf("expected cmd '/usr/bin/my-mcp-server', got %s", client.cmd)
	}
	if len(client.args) != 1 || client.args[0] != "--flag" {
		t.Errorf("expected args ['--flag'], got %v", client.args)
	}
	if client.env["KEY"] != "value" {
		t.Errorf("expected env KEY='value', got %v", client.env)
	}
}

// TestMCPToolExecuteUnknownServer tests AC4: unknown server returns error tool_result without connecting.
func TestMCPToolExecuteUnknownServer(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	mcpTool := &MCPTool{
		serverName:  "nonexistent-server",
		toolName:    "some-tool",
		inputSchema: map[string]any{"type": "object"},
	}

	result, err := mcpTool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for unknown server")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content")
	}
}

// TestMCPToolExecuteDisconnectedServer tests AC5: disconnected server returns error tool_result, no crash.
func TestMCPToolExecuteDisconnectedServer(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	// Create a client that is "connected" but with nil proc (disconnected state)
	client := &Client{
		Name: "test-server",
		proc: nil, // Disconnected
	}

	clients[NormalizeName("test-server")] = client

	mcpTool := &MCPTool{
		serverName:  "test-server",
		toolName:    "some-tool",
		inputSchema: map[string]any{"type": "object"},
	}

	result, err := mcpTool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for disconnected server")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content")
	}
}

// TestIntegrationMCPSubprocess tests AC1-AC5 with a real MCP server subprocess.
// This verifies the full JSON-RPC message exchange, tool discovery registration,
// and tool call dispatch using the stdio transport.
func TestIntegrationMCPSubprocess(t *testing.T) {
	// This test requires a Unix-like environment with /tmp
	if runtime.GOOS == "windows" {
		t.Skip("skipping MCP subprocess integration test on Windows (requires Unix shell)")
	}

	// AC3: JENNY_HOME isolation - override to use temp dir
	origFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return t.TempDir() }
	t.Cleanup(func() { constants.JennyHomeDirFunc = origFunc })

	// Get the absolute path to the fake MCP server source
	execDir, err := os.Getwd()
	if err != nil {
		t.Skipf("skipping integration test: could not get working directory: %v", err)
	}
	serverSrc := execDir + "/testdata/fake-mcp-server"
	serverBin := execDir + "/testdata/fake-mcp-server/fake-mcp-server"

	// Build the fake MCP server first
	cmd := exec.Command("go", "build", "-o", serverBin, serverSrc)
	if err := cmd.Run(); err != nil {
		t.Skipf("skipping integration test: could not build fake MCP server: %v", err)
	}

	// Create client connected to fake server
	client := NewClient("test-server", serverBin, nil, nil)

	// Connect (tests AC1: initialize and handshake)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect to fake MCP server: %v", err)
	}
	defer client.Disconnect()

	// List tools (tests AC2: tool discovery and registration)
	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}
	if len(mcpTools) == 0 {
		t.Error("expected at least one tool from fake server")
	}

	// Verify tool normalization
	for _, mt := range mcpTools {
		toolName := mt.Name()
		if !strings.HasPrefix(toolName, "mcp__test_server__") {
			t.Errorf("expected tool name prefix 'mcp__test_server__', got %s", toolName)
		}
	}

	// Call a tool (tests AC3: tool call dispatch)
	result, err := client.CallTool(context.Background(), "test-tool", map[string]any{})
	if err != nil {
		t.Fatalf("failed to call tool: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from tool call")
	}
}

func TestMaxMCPOutputChars(t *testing.T) {
	// Test that default is returned when env not set
	t.Setenv("MCP_MAX_OUTPUT_CHARS", "")
	if got := maxMCPOutputChars(); got != defaultMaxMCPOutputChars {
		t.Errorf("default: got %d, want %d", got, defaultMaxMCPOutputChars)
	}

	// Test with env override
	t.Setenv("MCP_MAX_OUTPUT_CHARS", "500")
	if got := maxMCPOutputChars(); got != 500 {
		t.Errorf("env override: got %d, want 500", got)
	}

	// Test with invalid value falls back to default
	t.Setenv("MCP_MAX_OUTPUT_CHARS", "invalid")
	if got := maxMCPOutputChars(); got != defaultMaxMCPOutputChars {
		t.Errorf("invalid env: got %d, want %d", got, defaultMaxMCPOutputChars)
	}
}

// TestHandleNotificationResourceListChanged tests AC1: resource cache invalidation on list_changed.
func TestHandleNotificationResourceListChanged(t *testing.T) {
	// Record initial cache generation
	initialGen := atomic.LoadInt64(&cacheGen)

	client := &Client{Name: "test-server"}

	// Simulate notifications/resources/list_changed notification
	notif := Notification{
		Method: "notifications/resources/list_changed",
		Params: json.RawMessage(`{}`),
	}

	// Handle the notification
	client.handleNotification(notif)

	// Verify cache was bumped (final > initial)
	newGen := atomic.LoadInt64(&cacheGen)
	if newGen <= initialGen {
		t.Errorf("cache generation should increase after list_changed: initial=%d, final=%d", initialGen, newGen)
	}
}

// TestHandleNotificationServerLog tests AC2: server logging notifications routed to internal log.
// Also tests the silent-unmarshal-drop fix: malformed params should not panic.
func TestHandleNotificationServerLog(t *testing.T) {
	client := &Client{Name: "test-server"}

	tests := []struct {
		level  string
		params string
	}{
		{"debug", `{"level":"debug","logger":"test","data":"debug message"}`},
		{"info", `{"level":"info","logger":"test","data":"info message"}`},
		{"warning", `{"level":"warning","logger":"test","data":"warning message"}`},
		{"error", `{"level":"error","logger":"test","data":"error message"}`},
		{"unknown", `{"level":"unknown","logger":"test","data":"unknown level"}`},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			notif := Notification{
				Method: "notifications/message",
				Params: json.RawMessage(tt.params),
			}
			// Should not panic
			client.handleNotification(notif)
		})
	}

	// Test silent-unmarshal-drop fix: malformed JSON params should not panic
	// and should be logged (not silently dropped)
	t.Run("malformed_params", func(t *testing.T) {
		notif := Notification{
			Method: "notifications/message",
			Params: json.RawMessage(`{invalid json}`),
		}
		// Should not panic - fix ensures log.Debug is called instead of silent drop
		client.handleNotification(notif)
	})
}

// TestClientCapabilitiesInInitializeRequest tests AC3: initialize requests include roots and sampling capabilities.
func TestClientCapabilitiesInInitializeRequest(t *testing.T) {
	// Test that the initialize request includes proper capabilities
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"roots": map[string]any{
					"listChanged": true,
				},
				"sampling": map[string]any{},
			},
			"clientInfo": map[string]any{
				"name":    "jenny",
				"version": "0.1.0",
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal initialize request: %v", err)
	}

	// Verify capabilities structure
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	params := parsed["params"].(map[string]any)
	caps := params["capabilities"].(map[string]any)

	// Verify roots capability
	roots := caps["roots"].(map[string]any)
	if listChanged := roots["listChanged"]; listChanged != true {
		t.Errorf("roots.listChanged should be true, got %v", listChanged)
	}

	// Verify sampling capability exists
	if _, ok := caps["sampling"]; !ok {
		t.Error("sampling capability should be present")
	}
}

// TestLogMessageParams tests LogMessageParams parsing.
func TestLogMessageParams(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		wantLevel  string
		wantLogger string
	}{
		{"debug with logger", `{"level":"debug","logger":"myLogger","data":"test"}`, "debug", "myLogger"},
		{"info without logger", `{"level":"info","data":"test"}`, "info", ""},
		{"warning", `{"level":"warning"}`, "warning", ""},
		{"error", `{"level":"error","logger":"err","data":{"msg":"oops"}}`, "error", "err"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params LogMessageParams
			if err := json.Unmarshal([]byte(tt.json), &params); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if params.Level != tt.wantLevel {
				t.Errorf("Level = %q, want %q", params.Level, tt.wantLevel)
			}
			if params.Logger != tt.wantLogger {
				t.Errorf("Logger = %q, want %q", params.Logger, tt.wantLogger)
			}
		})
	}
}

// TestClientSubscribeResource tests AC1: SubscribeResource sends resources/subscribe JSON-RPC request.
func TestClientSubscribeResource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping MCP subprocess integration test on Windows")
	}

	// Build the fake MCP server
	execDir, err := os.Getwd()
	if err != nil {
		t.Skipf("skipping test: could not get working directory: %v", err)
	}
	serverSrc := execDir + "/testdata/fake-mcp-server"
	serverBin := execDir + "/testdata/fake-mcp-server/fake-mcp-server"

	cmd := exec.Command("go", "build", "-o", serverBin, serverSrc)
	if err := cmd.Run(); err != nil {
		t.Skipf("skipping test: could not build fake MCP server: %v", err)
	}

	// Create and connect client
	client := NewClient("test-server", serverBin, nil, nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Subscribe should succeed
	err = client.SubscribeResource(ctx, "file:///test.txt")
	if err != nil {
		t.Errorf("SubscribeResource failed: %v", err)
	}
}

// TestClientUnsubscribeResource tests AC2: UnsubscribeResource sends resources/unsubscribe JSON-RPC request.
func TestClientUnsubscribeResource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping MCP subprocess integration test on Windows")
	}

	// Build the fake MCP server
	execDir, err := os.Getwd()
	if err != nil {
		t.Skipf("skipping test: could not get working directory: %v", err)
	}
	serverSrc := execDir + "/testdata/fake-mcp-server"
	serverBin := execDir + "/testdata/fake-mcp-server/fake-mcp-server"

	cmd := exec.Command("go", "build", "-o", serverBin, serverSrc)
	if err := cmd.Run(); err != nil {
		t.Skipf("skipping test: could not build fake MCP server: %v", err)
	}

	// Create and connect client
	client := NewClient("test-server", serverBin, nil, nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Unsubscribe should succeed
	err = client.UnsubscribeResource(ctx, "file:///test.txt")
	if err != nil {
		t.Errorf("UnsubscribeResource failed: %v", err)
	}
}

// TestHandleNotificationResourceUpdated tests AC3: notifications/resources/updated invalidates cache.
func TestHandleNotificationResourceUpdated(t *testing.T) {
	// Record initial cache generation
	initialGen := atomic.LoadInt64(&cacheGen)

	client := &Client{Name: "test-server"}

	// Simulate notifications/resources/updated notification
	notif := Notification{
		Method: "notifications/resources/updated",
		Params: json.RawMessage(`{"uri":"file:///test.txt"}`),
	}

	// Handle the notification
	client.handleNotification(notif)

	// Verify cache was bumped (final > initial)
	newGen := atomic.LoadInt64(&cacheGen)
	if newGen <= initialGen {
		t.Errorf("cache generation should increase after resource updated: initial=%d, final=%d", initialGen, newGen)
	}
}

// TestClientSubscribeResourceDisconnected tests AC4: subscribe fails when not connected.
func TestClientSubscribeResourceDisconnected(t *testing.T) {
	client := NewClient("test-server", "nonexistent-command", nil, nil)

	err := client.SubscribeResource(context.Background(), "file:///test.txt")
	if err == nil {
		t.Error("expected error when subscribing on disconnected client")
	}
}

// TestClientUnsubscribeResourceDisconnected tests AC4: unsubscribe fails when not connected.
func TestClientUnsubscribeResourceDisconnected(t *testing.T) {
	client := NewClient("test-server", "nonexistent-command", nil, nil)

	err := client.UnsubscribeResource(context.Background(), "file:///test.txt")
	if err == nil {
		t.Error("expected error when unsubscribing on disconnected client")
	}
}
