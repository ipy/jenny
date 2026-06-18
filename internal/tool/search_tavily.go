package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const tavilyDefaultURL = "https://api.tavily.com/search"

// TavilyProvider implements SearchClientProvider using the Tavily Search API.
type TavilyProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewTavilyProvider creates a new TavilyProvider.
func NewTavilyProvider(apiKey string) *TavilyProvider {
	return &TavilyProvider{
		apiKey:  apiKey,
		baseURL: tavilyDefaultURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// tavilyRequest is the JSON body sent to the Tavily API.
type tavilyRequest struct {
	Query string `json:"query"`
}

// tavilyResult is a single result from the Tavily API.
type tavilyResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// tavilyResponse is the JSON response from the Tavily API.
type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

// Search performs a web search via the Tavily API.
func (p *TavilyProvider) Search(ctx context.Context, query string) (*SearchResponse, error) {
	reqBody := tavilyRequest{Query: query}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("tavily: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("tavily: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tavily: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tavResp tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tavResp); err != nil {
		return nil, fmt.Errorf("tavily: decode response: %w", err)
	}

	results := make([]SearchResult, 0, len(tavResp.Results))
	for _, r := range tavResp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}

	return &SearchResponse{
		Query:   query,
		Results: results,
	}, nil
}
