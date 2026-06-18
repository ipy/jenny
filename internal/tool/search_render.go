package tool

import (
	"fmt"
	"strings"
)

// RenderSearchResponse converts a SearchResponse into tool result text.
// Both native and client results use this renderer to produce identical-format output.
func RenderSearchResponse(resp *SearchResponse) string {
	if resp == nil {
		return ""
	}
	if len(resp.Results) == 0 {
		return fmt.Sprintf("No results found for query: %q", resp.Query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Web search results for: %q\n\n", resp.Query))

	for i, r := range resp.Results {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		b.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		if r.Snippet != "" {
			b.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}
