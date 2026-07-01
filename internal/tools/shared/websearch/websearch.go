// Package websearch provides the web_search shared tool: a Go-native,
// Read-class tool that queries a configured search backend and returns ranked
// results (title, url, snippet). It is distinct from http_request — search
// DISCOVERS urls, http_request FETCHES a url the model already holds. The two
// stay separate and compose: this tool never fetches page bodies.
package websearch

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/search"
)

// maxResults caps the result-count the model may request, keeping the tool
// output bounded regardless of what the model asks for.
const maxResults = 20

// WebSearchTool exposes a search.Provider to the LLM as the web_search tool.
//
// The provider MAY be nil: when web search is unconfigured the tool stays
// registered and advertised (exposed-and-deny) and Execute returns a
// no-backend-configured tool-error rather than being hidden.
type WebSearchTool struct {
	provider search.Provider
}

// NewWebSearchTool builds the web_search tool around a resolved provider. Pass
// the boot-resolved provider; nil is valid and yields the exposed-and-deny
// behavior.
func NewWebSearchTool(provider search.Provider) *WebSearchTool {
	return &WebSearchTool{provider: provider}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web via the operator-configured search engine and return ranked results (title, URL, and snippet only). Use this to DISCOVER URLs and sources; it never fetches page contents. To read a specific result, pass its URL to http_request. Read-only."
}

func (t *WebSearchTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"query": {
				Type:        "string",
				Description: "The search query.",
			},
			"count": {
				Type:        "integer",
				Description: fmt.Sprintf("Optional maximum number of results to return (1-%d). Omit for the backend default.", maxResults),
			},
		},
		Required: []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	// Exposed-and-deny: the tool is always advertised, but with no backend
	// configured the call returns a tool-error result instead of a request.
	if t.provider == nil {
		return nil, fmt.Errorf("no search backend configured (set web_search.provider and base_url, e.g. a SearXNG instance)")
	}

	count := 0
	if c, ok := args["count"].(float64); ok && c > 0 {
		count = int(c)
	}
	if count > maxResults {
		count = maxResults
	}

	results, err := t.provider.Search(ctx, query, count)
	if err != nil {
		return nil, err
	}

	return formatResults(query, results), nil
}

// formatResults renders ranked results as readable text for the model. Only
// title, url, and snippet are presented — never page bodies.
func formatResults(query string, results []search.Result) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results for %q.", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q:\n", query)
	for i, r := range results {
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "\n%d. %s\n   %s\n", i+1, title, r.URL)
		if snippet := strings.TrimSpace(r.Snippet); snippet != "" {
			fmt.Fprintf(&b, "   %s\n", snippet)
		}
	}
	return b.String()
}
