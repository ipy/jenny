package tool

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ipy/jenny/internal/mcp"
)

// CallMcpTool provides dynamic MCP tool invocation. Instead of registering
// individual MCP tools, this single tool delegates to connected MCP servers
// at call time.
type CallMcpTool struct {
	sessionID string
}

// NewCallMcpTool creates a new CallMcpTool.
func NewCallMcpTool() *CallMcpTool {
	return &CallMcpTool{}
}

// WithSessionID sets the session ID for spill directory scoping.
func (t *CallMcpTool) WithSessionID(id string) *CallMcpTool {
	t.sessionID = id
	return t
}

func (t *CallMcpTool) Name() string { return "call_mcp" }

func (t *CallMcpTool) Description() string {
	return "Call a tool on a connected MCP server. Use this to invoke any MCP tool by specifying the server name and tool name."
}

func (t *CallMcpTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server": map[string]any{
				"type":        "string",
				"description": "The MCP server name",
			},
			"tool": map[string]any{
				"type":        "string",
				"description": "The tool name on the MCP server",
			},
			"arguments": map[string]any{
				"type":        "object",
				"description": "Arguments to pass to the MCP tool",
			},
		},
		"required": []string{"server", "tool"},
	}
}

func (t *CallMcpTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	serverName, _ := input["server"].(string)
	toolName, _ := input["tool"].(string)

	if serverName == "" {
		servers := mcp.GetConnectedServerNames()
		sort.Strings(servers)
		return &ToolResult{
			Content: fmt.Sprintf("Error: 'server' parameter is required. Available servers: %s", formatServerList(servers)),
			IsError: true,
		}, nil
	}

	if toolName == "" {
		return &ToolResult{
			Content: "Error: 'tool' parameter is required",
			IsError: true,
		}, nil
	}

	client := mcp.GetClient(serverName)
	if client == nil {
		servers := mcp.GetConnectedServerNames()
		sort.Strings(servers)
		return &ToolResult{
			Content: fmt.Sprintf("Error: MCP server %q not found. Available servers: %s", serverName, formatServerList(servers)),
			IsError: true,
		}, nil
	}

	arguments, _ := input["arguments"].(map[string]any)

	result, err := client.CallTool(ctx, toolName, arguments)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error calling MCP tool %q on server %q: %v", toolName, serverName, err),
			IsError: true,
		}, nil
	}

	return ApplySpillToToolResult(result, spillConfig(t.sessionID, cwd), false), nil
}

func formatServerList(servers []string) string {
	if len(servers) == 0 {
		return "(none)"
	}
	return strings.Join(servers, ", ")
}
