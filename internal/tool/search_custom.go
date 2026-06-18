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

// CustomProvider implements SearchClientProvider using a user-configured HTTP endpoint.
// The endpoint receives a POST with a JSON body and returns a SearchResponse-compatible JSON payload.
type CustomProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewCustomProvider creates a new CustomProvider.
func NewCustomProvider(baseURL, apiKey string) *CustomProvider {
	return &CustomProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// customRequest is the JSON body sent to a custom search endpoint.
type customRequest struct {
	Query string `json:"query"`
}

// Search performs a web search via the custom HTTP endpoint.
// The endpoint receives a POST with JSON body {"query": "..."}
// and must return a JSON body matching SearchResponse.
// The API key is sent only in the Authorization header.
func (p *CustomProvider) Search(ctx context.Context, query string) (*SearchResponse, error) {
	reqBody := customRequest{
		Query: query,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("custom: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("custom: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("custom: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("custom: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("custom: decode response: %w", err)
	}

	searchResp.Query = query
	return &searchResp, nil
}
