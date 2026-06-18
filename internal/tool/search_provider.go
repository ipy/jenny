package tool

import "context"

// SearchResult represents a single search result item.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchResponse holds the results from a web search.
type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// SearchClientProvider performs web search via a third-party client API.
type SearchClientProvider interface {
	Search(ctx context.Context, query string) (*SearchResponse, error)
}

// NativeSearchRunner performs a native web search through the active provider.
// Implementations are provider-specific (e.g., Anthropic web_search_20250305,
// OpenAI web_search tool, Gemini Google Search grounding).
type NativeSearchRunner interface {
	RunNativeSearch(ctx context.Context, query string) (*SearchResponse, error)
}
