package tool

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/lsp"
)

const (
	// maxFileSize is the maximum file size for LSP operations (10 MB).
	maxFileSize = 10 * 1024 * 1024
)

// lspManager is the interface LSPTool uses to interact with the LSP system.
// *lsp.ServerManager satisfies this interface.
type lspManager interface {
	Connected() bool
	IsFileOpen(path string) bool
	OpenFile(path, content string) error
	ChangeFile(path, content string) error
	SaveFile(path string) error
	CloseFile(path string) error
	GoToDefinition(ctx context.Context, uri string, line, character int) ([]lsp.Location, error)
	FindReferences(ctx context.Context, uri string, line, character int) ([]lsp.Reference, error)
	Hover(ctx context.Context, uri string, line, character int) (*lsp.HoverResult, error)
	DocumentSymbol(ctx context.Context, uri string) ([]lsp.DocumentSymbol, error)
	WorkspaceSymbol(ctx context.Context, query string) ([]lsp.WorkspaceSymbol, error)
	GoToImplementation(ctx context.Context, uri string, line, character int) ([]lsp.Location, error)
	PrepareCallHierarchy(ctx context.Context, uri string, line, character int) ([]lsp.CallHierarchyItem, error)
	IncomingCalls(ctx context.Context, uri string, line, character int) ([]lsp.CallHierarchyItem, error)
	OutgoingCalls(ctx context.Context, uri string, line, character int) ([]lsp.CallHierarchyItem, error)
}

// LSPTool provides Language Server Protocol operations for code intelligence.
type LSPTool struct {
	manager lspManager
}

// NewLSPTool creates a new LSP tool with the given server manager.
func NewLSPTool(manager *lsp.ServerManager) *LSPTool {
	return &LSPTool{manager: manager}
}

// newLSPToolForTesting creates a new LSP tool with any lspManager implementation.
func newLSPToolForTesting(manager lspManager) *LSPTool {
	return &LSPTool{manager: manager}
}

// Name returns the tool name.
func (t *LSPTool) Name() string {
	return "lsp"
}

// Description returns a description of the tool.
func (t *LSPTool) Description() string {
	return "Language Server Protocol operations for code intelligence. " +
		"Provides goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, " +
		"goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls. " +
		"Coordinates are 1-based (editor style). Requires LSP server connected."
}

// InputSchema returns the JSON schema for tool input.
// Coordinates are 1-based (editor convention), converted to LSP 0-based internally.
func (t *LSPTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"description": "LSP operation to perform: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls",
			},
			"uri": map[string]any{
				"type":        "string",
				"description": "File URI (file:///path/to/file)",
			},
			"line": map[string]any{
				"type":        "integer",
				"description": "1-based line number (editor convention, converted to LSP 0-based internally)",
			},
			"character": map[string]any{
				"type":        "integer",
				"description": "1-based character offset (editor convention, converted to LSP 0-based internally)",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Query string for workspaceSymbol search",
			},
		},
		"required": []string{"operation"},
	}
}

// Execute runs the LSP tool with the given input.
func (t *LSPTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Check if any LSP server is connected
	if !t.manager.Connected() {
		return &ToolResult{
			Content: "LSP server not connected.",
			IsError: true,
		}, nil
	}

	// Get operation name
	operation, ok := input["operation"].(string)
	if !ok || operation == "" {
		return &ToolResult{
			Content: "operation is required",
			IsError: true,
		}, nil
	}

	// Validate operation name
	validOperations := map[string]bool{
		"goToDefinition":       true,
		"findReferences":       true,
		"hover":                true,
		"documentSymbol":       true,
		"workspaceSymbol":      true,
		"goToImplementation":   true,
		"prepareCallHierarchy": true,
		"incomingCalls":        true,
		"outgoingCalls":        true,
	}
	if !validOperations[operation] {
		return &ToolResult{
			Content: fmt.Sprintf("unknown operation %q, valid operations: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls", operation),
			IsError: true,
		}, nil
	}

	// Get file path for file size check (if applicable)
	var filePath string
	if uri, ok := input["uri"].(string); ok && uri != "" {
		filePath = uriToFilePath(uri)
	}

	// Check file size before any LSP operation
	if filePath != "" {
		info, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				// File doesn't exist, let the LSP server handle the error
			} else {
				return &ToolResult{
					Content: fmt.Sprintf("failed to stat file: %v", err),
					IsError: true,
				}, nil
			}
		} else if info.Size() > maxFileSize {
			return &ToolResult{
				Content: fmt.Sprintf("file too large (%d bytes > %d byte limit)", info.Size(), maxFileSize),
				IsError: true,
			}, nil
		}
	}

	// Parse 1-based coordinates and convert to 0-based for LSP client
	var line, character int
	switch l := input["line"].(type) {
	case int:
		line = l - 1
	case float64:
		line = int(l) - 1
	}
	switch c := input["character"].(type) {
	case int:
		character = c - 1
	case float64:
		character = int(c) - 1
	}

	// Ensure file is open in LSP server for file-based operations
	if filePath != "" && operation != "workspaceSymbol" {
		if !t.manager.IsFileOpen(filePath) {
			content, err := os.ReadFile(filePath)
			if err == nil {
				_ = t.manager.OpenFile(filePath, string(content))
			}
		}
	}

	// Execute the operation
	switch operation {
	case "goToDefinition":
		return t.executeGoToDefinition(ctx, input, line, character)
	case "findReferences":
		return t.executeFindReferences(ctx, input, line, character, cwd)
	case "hover":
		return t.executeHover(ctx, input, line, character)
	case "documentSymbol":
		return t.executeDocumentSymbol(ctx, input)
	case "workspaceSymbol":
		return t.executeWorkspaceSymbol(ctx, input, cwd)
	case "goToImplementation":
		return t.executeGoToImplementation(ctx, input, line, character)
	case "prepareCallHierarchy":
		return t.executePrepareCallHierarchy(ctx, input, line, character)
	case "incomingCalls":
		return t.executeIncomingCalls(ctx, input, line, character)
	case "outgoingCalls":
		return t.executeOutgoingCalls(ctx, input, line, character)
	default:
		return &ToolResult{
			Content: fmt.Sprintf("unhandled operation %q", operation),
			IsError: true,
		}, nil
	}
}

// executeGoToDefinition executes the goToDefinition operation.
func (t *LSPTool) executeGoToDefinition(ctx context.Context, input map[string]any, line, character int) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	locations, err := t.manager.GoToDefinition(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("goToDefinition failed: %v", err),
			IsError: true,
		}, nil
	}

	if len(locations) == 0 {
		return &ToolResult{
			Content: "no definition found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("definitions:\n")
	for _, loc := range locations {
		sb.WriteString(fmt.Sprintf("  %s:%d:%d\n", loc.URI, loc.Line+1, loc.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executeFindReferences executes the findReferences operation.
func (t *LSPTool) executeFindReferences(ctx context.Context, input map[string]any, line, character int, cwd string) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	references, err := t.manager.FindReferences(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("findReferences failed: %v", err),
			IsError: true,
		}, nil
	}

	// Filter gitignored paths
	repoRoot, err := git.GetRoot(cwd)
	if err == nil {
		var filtered []lsp.Reference
		for _, ref := range references {
			filePath := uriToFilePath(ref.URI)
			ignored, _ := git.IsIgnored(repoRoot, filePath)
			if !ignored {
				filtered = append(filtered, ref)
			}
		}
		references = filtered
	}

	if len(references) == 0 {
		return &ToolResult{
			Content: "no references found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("references:\n")
	for _, ref := range references {
		sb.WriteString(fmt.Sprintf("  %s:%d:%d\n", ref.URI, ref.Line+1, ref.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executeHover executes the hover operation.
func (t *LSPTool) executeHover(ctx context.Context, input map[string]any, line, character int) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	result, err := t.manager.Hover(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("hover failed: %v", err),
			IsError: true,
		}, nil
	}

	if result == nil {
		return &ToolResult{
			Content: "no hover information available",
			IsError: false,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("%s (line %d, char %d)", result.Contents, result.Line+1, result.Start+1),
		IsError: false,
	}, nil
}

// executeDocumentSymbol executes the documentSymbol operation.
func (t *LSPTool) executeDocumentSymbol(ctx context.Context, input map[string]any) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	symbols, err := t.manager.DocumentSymbol(ctx, uri)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("documentSymbol failed: %v", err),
			IsError: true,
		}, nil
	}

	if len(symbols) == 0 {
		return &ToolResult{
			Content: "no symbols found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("symbols:\n")
	for _, sym := range symbols {
		indent := ""
		if sym.Parent != "" {
			indent = "  "
		}
		sb.WriteString(fmt.Sprintf("%s%s %s (line %d)\n", indent, sym.Kind, sym.Name, sym.Line+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executeWorkspaceSymbol executes the workspaceSymbol operation.
func (t *LSPTool) executeWorkspaceSymbol(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	query, _ := input["query"].(string)
	symbols, err := t.manager.WorkspaceSymbol(ctx, query)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("workspaceSymbol failed: %v", err),
			IsError: true,
		}, nil
	}

	// Filter gitignored paths
	repoRoot, err := git.GetRoot(cwd)
	if err == nil {
		var filtered []lsp.WorkspaceSymbol
		for _, sym := range symbols {
			ignored, _ := git.IsIgnored(repoRoot, sym.FilePath)
			if !ignored {
				filtered = append(filtered, sym)
			}
		}
		symbols = filtered
	}

	if len(symbols) == 0 {
		return &ToolResult{
			Content: "no symbols found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("workspace symbols:\n")
	for _, sym := range symbols {
		sb.WriteString(fmt.Sprintf("  %s %s (%s:%d:%d)\n", sym.Kind, sym.Name, sym.URI, sym.Line+1, sym.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executeGoToImplementation executes the goToImplementation operation.
func (t *LSPTool) executeGoToImplementation(ctx context.Context, input map[string]any, line, character int) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	locations, err := t.manager.GoToImplementation(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("goToImplementation failed: %v", err),
			IsError: true,
		}, nil
	}

	if len(locations) == 0 {
		return &ToolResult{
			Content: "no implementation found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("implementations:\n")
	for _, loc := range locations {
		sb.WriteString(fmt.Sprintf("  %s:%d:%d\n", loc.URI, loc.Line+1, loc.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executePrepareCallHierarchy executes the prepareCallHierarchy operation.
func (t *LSPTool) executePrepareCallHierarchy(ctx context.Context, input map[string]any, line, character int) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	items, err := t.manager.PrepareCallHierarchy(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("prepareCallHierarchy failed: %v", err),
			IsError: true,
		}, nil
	}

	if len(items) == 0 {
		return &ToolResult{
			Content: "no call hierarchy items found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("call hierarchy:\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("  %s %s (%s:%d:%d)\n", item.Kind, item.Name, item.URI, item.Line+1, item.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executeIncomingCalls executes the incomingCalls operation.
func (t *LSPTool) executeIncomingCalls(ctx context.Context, input map[string]any, line, character int) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	items, err := t.manager.IncomingCalls(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("incomingCalls failed: %v", err),
			IsError: true,
		}, nil
	}

	if len(items) == 0 {
		return &ToolResult{
			Content: "no incoming calls found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("incoming calls:\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("  %s %s (%s:%d:%d)\n", item.Kind, item.Name, item.URI, item.Line+1, item.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// executeOutgoingCalls executes the outgoingCalls operation.
func (t *LSPTool) executeOutgoingCalls(ctx context.Context, input map[string]any, line, character int) (*ToolResult, error) {
	uri, _ := input["uri"].(string)
	items, err := t.manager.OutgoingCalls(ctx, uri, line, character)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("outgoingCalls failed: %v", err),
			IsError: true,
		}, nil
	}

	if len(items) == 0 {
		return &ToolResult{
			Content: "no outgoing calls found",
			IsError: false,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString("outgoing calls:\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("  %s %s (%s:%d:%d)\n", item.Kind, item.Name, item.URI, item.Line+1, item.Start+1))
	}
	return &ToolResult{
		Content: sb.String(),
		IsError: false,
	}, nil
}

// uriToFilePath converts a file URI to a file path.
func uriToFilePath(uri string) string {
	if after, ok := strings.CutPrefix(uri, "file://"); ok {
		path := after
		// Handle Windows-style paths: file:///C:/path -> /C:/path -> C:/path
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		return path
	}
	return uri
}
