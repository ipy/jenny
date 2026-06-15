package tool

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// WebSearch limits.
const (
	webSearchMinQueryLen      = 2 // AC2: minimum query length
	webSearchMaxResults       = 8 // AC3: maximum results per call
	webSearchMaxCallsPerAgent = 8 // maximum searches per agent session
)

// WebSearchMaxResults is the maximum results per call for web search.
const WebSearchMaxResults = webSearchMaxResults

// supportedWebSearchModels contains model prefixes that support web search.
// These are first-party Claude models and their cloud equivalents.
var supportedWebSearchModels = []string{
	"claude-4",
	"claude-3.5",
	"claude-3",
}

// isModelSupported checks if the given model supports server-side web search.
func isModelSupported(model string) bool {
	lower := strings.ToLower(model)
	for _, prefix := range supportedWebSearchModels {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// WebSearchTool provides server-side web search via the Anthropic API's
// web_search_20250305 tool schema.
type WebSearchTool struct {
	model     string // model name for gating check
	callCount atomic.Int32
}

// NewWebSearchTool creates a new WebSearchTool.
func NewWebSearchTool(model string) *WebSearchTool {
	return &WebSearchTool{model: model}
}

// Name returns the tool name.
func (t *WebSearchTool) Name() string {
	return "web_search"
}

// Description returns a description of the tool.
func (t *WebSearchTool) Description() string {
	return "Search the web using server-side search. Returns search results with titles, URLs, and snippets. " +
		"Query must be at least 2 characters. Maximum 8 results per search. " +
		"Use allowed_domains or blocked_domains to filter results (mutually exclusive)."
}

// InputSchema returns the JSON schema for tool input.
// This registers the web_search_20250305 tool schema with the API.
func (t *WebSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query (minimum 2 characters)",
			},
			"allowed_domains": map[string]any{
				"type":        "array",
				"description": "Restrict search results to these domains (mutually exclusive with blocked_domains)",
				"items":       map[string]any{"type": "string"},
			},
			"blocked_domains": map[string]any{
				"type":        "array",
				"description": "Exclude results from these domains (mutually exclusive with allowed_domains)",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"query"},
	}
}

// Execute validates the search inputs and returns a result.
// Note: The actual search is performed server-side by the API's web_search_20250305 tool.
// This Execute() call validates inputs and handles local error cases.
// AC4 (model gating) is checked here; AC5 (server errors) are surfaced by the API.
func (t *WebSearchTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Enforce max searches per agent session
	count := t.callCount.Add(1)
	if int(count) > webSearchMaxCallsPerAgent {
		return &ToolResult{
			Content: fmt.Sprintf("Maximum web searches per session reached (%d). Use previously fetched results or web_fetch for specific URLs.", webSearchMaxCallsPerAgent),
			IsError: true,
		}, nil
	}

	// AC4: Check model support
	if !isModelSupported(t.model) {
		return &ToolResult{
			Content: fmt.Sprintf("Web search is not supported on model '%s'. Supported models include claude-4, claude-3.5, and their Vertex/Foundry equivalents.", t.model),
			IsError: true,
		}, nil
	}

	// AC2: Validate query length
	query, ok := input["query"].(string)
	if !ok || len(query) < webSearchMinQueryLen {
		return &ToolResult{
			Content: fmt.Sprintf("Query must be at least %d characters", webSearchMinQueryLen),
			IsError: true,
		}, nil
	}

	// AC3: Check for mutual exclusion of domain filters
	hasAllowed := false
	hasBlocked := false

	if allowed, ok := input["allowed_domains"].([]any); ok && len(allowed) > 0 {
		hasAllowed = true
	}
	if blocked, ok := input["blocked_domains"].([]any); ok && len(blocked) > 0 {
		hasBlocked = true
	}

	if hasAllowed && hasBlocked {
		return &ToolResult{
			Content: "allowed_domains and blocked_domains are mutually exclusive. Use one or the other, not both.",
			IsError: true,
		}, nil
	}

	// All validations passed.
	// The API will handle the actual search server-side and return results.
	// This tool_use result indicates the search request was made.
	return &ToolResult{
		Content: fmt.Sprintf("Web search executed: %q (results handled server-side)", query),
		IsError: false,
	}, nil
}
