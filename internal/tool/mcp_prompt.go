package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/toolresult"
)

// McpPromptTool retrieves parameterized prompts from MCP servers.
type McpPromptTool struct{}

// NewMcpPromptTool creates a new McpPromptTool.
func NewMcpPromptTool() *McpPromptTool {
	return &McpPromptTool{}
}

// Name returns the tool name.
func (t *McpPromptTool) Name() string {
	return "McpPrompt"
}

// Description returns a description of the tool.
func (t *McpPromptTool) Description() string {
	return "List or retrieve parameterized prompts from connected MCP servers."
}

// InputSchema returns the JSON schema for tool input.
func (t *McpPromptTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: 'list' to see available prompts, 'get' to retrieve a specific prompt",
				"enum":        []any{"list", "get"},
			},
			"server": map[string]any{
				"type":        "string",
				"description": "Normalized MCP server name (required for 'get')",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Prompt name as defined on the server (required for 'get')",
			},
			"arguments": map[string]any{
				"type":        "object",
				"description": "Optional arguments for the prompt (used with 'get')",
			},
		},
		"required": []string{"action"},
	}
}

// Execute performs the requested action (list or get prompt).
func (t *McpPromptTool) Execute(ctx context.Context, input map[string]any, _ string) (*toolresult.ToolResult, error) {
	action, _ := input["action"].(string)

	if action == "list" {
		prompts := mcp.GetPrompts()
		if len(prompts) == 0 {
			return &toolresult.ToolResult{
				Content: "No MCP prompts available",
				IsError: false,
			}, nil
		}

		var sb strings.Builder
		sb.WriteString("Available MCP Prompts:\n")
		for _, p := range prompts {
			fmt.Fprintf(&sb, "- %s (server: %s): %s\n", p.Name, p.ServerName, p.Description)
			if len(p.Arguments) > 0 {
				sb.WriteString("  Arguments:\n")
				for _, arg := range p.Arguments {
					requiredStr := ""
					if arg.Required {
						requiredStr = " (required)"
					}
					fmt.Fprintf(&sb, "    - %s: %s%s\n", arg.Name, arg.Description, requiredStr)
				}
			}
		}
		return &toolresult.ToolResult{
			Content: sb.String(),
			IsError: false,
		}, nil
	}

	server, _ := input["server"].(string)
	name, _ := input["name"].(string)
	args, _ := input["arguments"].(map[string]any)

	if server == "" || name == "" {
		return &toolresult.ToolResult{
			Content: "Error: 'server' and 'name' are required for 'get' action",
			IsError: true,
		}, nil
	}

	client := mcp.GetClient(server)
	if client == nil {
		return &toolresult.ToolResult{
			Content: fmt.Sprintf("Error: MCP server '%s' not found", server),
			IsError: true,
		}, nil
	}

	content, err := client.GetPrompt(ctx, name, args)
	if err != nil {
		return &toolresult.ToolResult{
			Content: fmt.Sprintf("Error getting MCP prompt: %v", err),
			IsError: true,
		}, nil
	}

	return &toolresult.ToolResult{
		Content: content,
		IsError: false,
	}, nil
}
