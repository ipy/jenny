package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRealWorldHTTPMCP connects to a real public MCP HTTP server.
// Skipped unless MCP_REALWORLD_TEST=1 is set (to avoid CI flakiness from network).
// Can also be run with: MCP_REALWORLD_TEST=1 go test ./internal/mcp/ -run TestRealWorldHTTPMCP -v
func TestRealWorldHTTPMCP(t *testing.T) {
	if os.Getenv("MCP_REALWORLD_TEST") == "" {
		t.Skip("skipping real-world test (set MCP_REALWORLD_TEST=1 to run)")
	}

	serverURL := os.Getenv("MCP_REALWORLD_URL")
	if serverURL == "" {
		serverURL = "https://mcp.deepwiki.com/mcp"
	}

	client := NewHTTPClient("http-mcp-server", serverURL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// AC2: Connect and initialize
	t.Run("Connect", func(t *testing.T) {
		if err := client.Connect(ctx); err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
	})
	defer client.Disconnect()

	// AC9: Discover tools
	t.Run("ListTools", func(t *testing.T) {
		tools, err := client.ListTools(ctx)
		if err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}
		if len(tools) == 0 {
			t.Fatal("expected at least one tool from server")
		}

		t.Logf("discovered %d tools:", len(tools))
		for _, tool := range tools {
			t.Logf("  - %s", tool.Name())
		}

		// Verify tool naming convention uses normalized server name
		expectedPrefix := "mcp__" + NormalizeName("http-mcp-server") + "__"
		for _, tool := range tools {
			name := tool.Name()
			if !strings.HasPrefix(name, expectedPrefix) {
				t.Errorf("unexpected tool name prefix: %s (expected %s)", name, expectedPrefix)
			}
		}
	})

	// Call a tool
	t.Run("CallTool", func(t *testing.T) {
		tools, err := client.ListTools(ctx)
		if err != nil {
			t.Skipf("cannot list tools: %v", err)
		}
		if len(tools) == 0 {
			t.Skip("no tools available to call")
		}

		// Use the first available tool with whatever args it needs
		toolName := tools[0].toolName
		t.Logf("calling tool: %s", toolName)

		// For DeepWiki's read_wiki_structure, pass a known repo
		args := map[string]any{}
		if toolName == "read_wiki_structure" {
			args["repoName"] = "anthropics/anthropic-cookbook"
		} else if toolName == "ask_question" {
			args["repoName"] = "anthropics/anthropic-cookbook"
			args["question"] = "what is this repo about"
		}

		result, err := client.CallTool(toolName, args)
		if err != nil {
			t.Fatalf("CallTool(%s) failed: %v", toolName, err)
		}
		t.Logf("tool result (first 200 chars): %.200s", result)
		if result == "" {
			t.Error("expected non-empty result")
		}
	})

	// List resources
	t.Run("ListResources", func(t *testing.T) {
		resources, err := client.ListResources(ctx)
		if err != nil {
			// Some servers don't support resources - that's fine
			t.Logf("ListResources: %v (may not be supported)", err)
			return
		}
		t.Logf("discovered %d resources", len(resources))
	})
}

// TestRealWorldHTTPMCPSessionManagement tests session ID handling with real server.
func TestRealWorldHTTPMCPSessionManagement(t *testing.T) {
	if os.Getenv("MCP_REALWORLD_TEST") == "" {
		t.Skip("skipping real-world test (set MCP_REALWORLD_TEST=1 to run)")
	}

	serverURL := os.Getenv("MCP_REALWORLD_URL")
	if serverURL == "" {
		serverURL = "https://mcp.deepwiki.com/mcp"
	}

	transport := NewHTTPTransport(serverURL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize
	initReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "jenny-test", "version": "0.1.0"},
		},
	}

	resp, err := transport.SendRequest(ctx, initReq)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %v", resp.Error)
	}

	var initResult initializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		t.Fatalf("failed to parse initialize result: %v", err)
	}
	t.Logf("server: %s@%s, protocol: %s",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version, initResult.ProtocolVersion)

	// Send initialized notification
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := transport.SendNotification(ctx, notif); err != nil {
		t.Logf("notification warning (non-fatal): %v", err)
	}

	// List tools (using session)
	toolsReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      int64(2),
		Method:  "tools/list",
	}
	resp, err = transport.SendRequest(ctx, toolsReq)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}

	var tools toolsListResult
	if err := json.Unmarshal(resp.Result, &tools); err != nil {
		t.Fatalf("failed to parse tools: %v", err)
	}
	t.Logf("tools: %d", len(tools.Tools))
	for _, tool := range tools.Tools {
		t.Logf("  %s: %s", tool.Name, tool.Description)
	}

	// Clean close
	if err := transport.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
