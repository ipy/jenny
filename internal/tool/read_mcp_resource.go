// Package tool provides tool implementations.
package tool

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/mcp"
)

// ReadMcpResourceTool reads a single MCP resource by server and URI.
type ReadMcpResourceTool struct {
	sessionID string
}

// NewReadMcpResourceTool creates a new ReadMcpResourceTool.
func NewReadMcpResourceTool() *ReadMcpResourceTool {
	return &ReadMcpResourceTool{}
}

// WithSessionID sets the session ID for the ReadMcpResourceTool.
func (t *ReadMcpResourceTool) WithSessionID(id string) *ReadMcpResourceTool {
	t.sessionID = id
	return t
}

// Name returns the tool name.
func (t *ReadMcpResourceTool) Name() string {
	return "read_mcp_resource"
}

// Description returns a description of the tool.
func (t *ReadMcpResourceTool) Description() string {
	return "List resource templates or read a single MCP resource by server and URI."
}

// InputSchema returns the JSON schema for tool input.
func (t *ReadMcpResourceTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: 'read' to fetch a resource, 'list_templates' to see available resource templates",
				"enum":        []any{"read", "list_templates"},
			},
			"server": map[string]any{
				"type":        "string",
				"description": "MCP server name (required for 'read')",
			},
			"uri": map[string]any{
				"type":        "string",
				"description": "Resource URI to read (required for 'read' if template is missing)",
			},
			"template": map[string]any{
				"type":        "string",
				"description": "Optional: Resource URI template to use if uri is not provided",
			},
			"arguments": map[string]any{
				"type":        "object",
				"description": "Optional: Arguments for the URI template",
			},
		},
		"required": []string{"action"},
	}
}

// Execute performs the requested action (read resource or list templates).
func (t *ReadMcpResourceTool) Execute(ctx context.Context, input map[string]any, _ string) (*ToolResult, error) {
	action, _ := input["action"].(string)

	if action == "list_templates" {
		templates := mcp.GetResourceTemplates()
		if len(templates) == 0 {
			return &ToolResult{
				Content: "No MCP resource templates available",
				IsError: false,
			}, nil
		}

		var sb strings.Builder
		sb.WriteString("Available MCP Resource Templates:\n")
		for _, templ := range templates {
			fmt.Fprintf(&sb, "- %s (server: %s): %s\n", templ.URITemplate, templ.ServerName, templ.Description)
		}
		return &ToolResult{
			Content: sb.String(),
			IsError: false,
		}, nil
	}

	server, _ := input["server"].(string)
	uri, _ := input["uri"].(string)
	template, _ := input["template"].(string)
	args, _ := input["arguments"].(map[string]any)

	if server == "" {
		return &ToolResult{
			Content: "Error: server parameter is required for 'read' action",
			IsError: true,
		}, nil
	}

	if uri == "" && template == "" {
		return &ToolResult{
			Content: "Error: either uri or template parameter is required for 'read' action",
			IsError: true,
		}, nil
	}

	// Expand template if provided and uri is missing
	if uri == "" && template != "" {
		uri = expandURITemplate(template, args)
	}

	// Validate server exists
	client := mcp.GetClient(server)
	if client == nil {
		clients := mcp.GetMCPClients()
		var available []string
		for name := range clients {
			available = append(available, name)
		}
		return &ToolResult{
			Content: fmt.Sprintf("MCP server '%s' not found. Available servers: %v", server, available),
			IsError: true,
		}, nil
	}

	// Fetch resource content
	contents, err := client.ReadResource(ctx, uri)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading resource: %v", err),
			IsError: true,
		}, nil
	}

	// Build output contents
	outputContents := make([]map[string]any, 0, len(contents))
	for _, c := range contents {
		item := map[string]any{
			"uri": uri,
		}
		if c.MimeType != "" {
			item["mimeType"] = c.MimeType
		}
		if c.Type == "text" {
			item["text"] = c.Text
		} else if c.Type == "blob" {
			// AC3: Binary content persisted to disk
			savedPath, err := t.persistBlob(c.Blob)
			if err != nil {
				// AC4: Persist failure does not inline base64
				return &ToolResult{
					Content: fmt.Sprintf("Error saving binary content to disk: %v", err),
					IsError: true,
				}, nil
			}
			item["blobSavedTo"] = savedPath
		}
		outputContents = append(outputContents, item)
	}

	jsonBytes, err := json.Marshal(map[string]any{"contents": outputContents})
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error marshaling result: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: string(jsonBytes),
		IsError: false,
	}, nil
}

// expandURITemplate performs simple {name} substitution for MCP URI templates.
func expandURITemplate(template string, args map[string]any) string {
	result := template
	for k, v := range args {
		placeholder := "{" + k + "}"
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", v))
	}
	return result
}

// persistBlob decodes base64 data and writes it to a unique file in ~/.jenny/mcp-resources/.
func (t *ReadMcpResourceTool) persistBlob(data []byte) (string, error) {
	// Generate unique filename: timestamp-random suffix
	timestamp := time.Now().UnixNano()
	var b [8]byte
	rand.Read(b[:])
	var randSuffix uint64
	for _, v := range b {
		randSuffix = randSuffix<<8 + uint64(v)
	}
	filename := fmt.Sprintf("%d-%016x.bin", timestamp, randSuffix)

	// Create persist directory
	persistDir := filepath.Join(constants.SessionDir(t.sessionID), "mcp-resources")
	if err := os.MkdirAll(persistDir, 0755); err != nil {
		return "", fmt.Errorf("creating persist directory: %w", err)
	}

	// Write file
	filePath := filepath.Join(persistDir, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("writing blob to disk: %w", err)
	}

	return filePath, nil
}
