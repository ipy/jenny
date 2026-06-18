package tool

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/ipy/jenny/internal/api"
)

// WebSearch limits.
const (
	webSearchMinQueryLen      = 2 // AC2: minimum query length
	webSearchMaxResults       = 8 // AC3: maximum results per call
	webSearchMaxCallsPerAgent = 8 // maximum searches per agent session
)

// WebSearchMaxResults is the maximum results per call for web search.
const WebSearchMaxResults = webSearchMaxResults

// WebSearchTool provides web search via native provider search or client fallback.
type WebSearchTool struct {
	config         *WebSearchConfig
	nativeRunner   NativeSearchRunner
	clientProvider SearchClientProvider
	provider       api.Provider
	callCount      atomic.Int32
}

// NewWebSearchTool creates a new WebSearchTool.
func NewWebSearchTool(config *WebSearchConfig, nativeRunner NativeSearchRunner, clientProvider SearchClientProvider) *WebSearchTool {
	return &WebSearchTool{
		config:         config,
		nativeRunner:   nativeRunner,
		clientProvider: clientProvider,
	}
}

// WithProvider sets the active provider for capability checks (SupportsNativeSearch).
func (t *WebSearchTool) WithProvider(p api.Provider) {
	t.provider = p
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

// Execute validates the search inputs and performs the search according to the configured strategy.
func (t *WebSearchTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Enforce max searches per agent session
	count := t.callCount.Add(1)
	if int(count) > webSearchMaxCallsPerAgent {
		return &ToolResult{
			Content: fmt.Sprintf("Maximum web searches per session reached (%d). Use previously fetched results or web_fetch for specific URLs.", webSearchMaxCallsPerAgent),
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

	// Strategy-based search execution
	strategy := StrategyNative
	if t.config != nil {
		strategy = t.config.Strategy
	}

	switch strategy {
	case StrategyDisabled:
		return &ToolResult{
			Content: "web search disabled",
			IsError: true,
		}, nil

	case StrategyClient:
		return t.executeClientSearch(ctx, query)

	default: // StrategyNative (or unknown — treat as native)
		return t.executeNativeSearch(ctx, query)
	}
}

// executeNativeSearch attempts native search, falling back to client provider on failure.
func (t *WebSearchTool) executeNativeSearch(ctx context.Context, query string) (*ToolResult, error) {
	// Gate on provider capability: if the active provider does not support
	// native search, fall back to client provider immediately.
	if t.provider == nil || !t.provider.SupportsNativeSearch() {
		return t.fallbackToClient(ctx, query)
	}

	if t.nativeRunner == nil {
		return t.fallbackToClient(ctx, query)
	}

	resp, err := t.nativeRunner.RunNativeSearch(ctx, query)
	if err != nil {
		return t.fallbackToClient(ctx, query)
	}

	return &ToolResult{
		Content: RenderSearchResponse(resp),
		IsError: false,
	}, nil
}

// executeClientSearch uses the configured client provider directly.
func (t *WebSearchTool) executeClientSearch(ctx context.Context, query string) (*ToolResult, error) {
	if t.clientProvider == nil {
		return &ToolResult{
			Content: "web search client provider not configured",
			IsError: true,
		}, nil
	}

	resp, err := t.clientProvider.Search(ctx, query)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("web search client error: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: RenderSearchResponse(resp),
		IsError: false,
	}, nil
}

// fallbackToClient attempts to use the client provider as a fallback.
func (t *WebSearchTool) fallbackToClient(ctx context.Context, query string) (*ToolResult, error) {
	if t.clientProvider == nil {
		return &ToolResult{
			Content: "web search unavailable: no native search support and no client provider configured",
			IsError: true,
		}, nil
	}

	resp, err := t.clientProvider.Search(ctx, query)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("web search fallback error: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: RenderSearchResponse(resp),
		IsError: false,
	}, nil
}
