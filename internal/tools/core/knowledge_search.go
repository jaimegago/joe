package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/llm"
)

// KnowledgeSearchClient is the interface for semantic knowledge search.
type KnowledgeSearchClient interface {
	SearchKnowledge(ctx context.Context, query string, topK int, tierFilter []knowledge.Tier) ([]knowledge.SearchResult, error)
}

// SearchKnowledgeTool performs semantic search over the Joe knowledge store.
// Tier: T1 (Observe) — read-only.
type SearchKnowledgeTool struct {
	client KnowledgeSearchClient
}

// NewSearchKnowledgeTool creates a new search_knowledge tool.
func NewSearchKnowledgeTool(c KnowledgeSearchClient) *SearchKnowledgeTool {
	return &SearchKnowledgeTool{client: c}
}

func (t *SearchKnowledgeTool) Name() string { return "search_knowledge" }

func (t *SearchKnowledgeTool) Description() string {
	return "Search the Joe knowledge store using semantic similarity. Returns relevant runbooks, patterns, insights, and synced documentation. Use this when you need context about how something works, known failure modes, or operational patterns."
}

func (t *SearchKnowledgeTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"query": {
				Type:        "string",
				Description: "Natural language search query, e.g. 'payment service timeout fix' or 'how to scale HPA'",
			},
			"top_k": {
				Type:        "integer",
				Description: "Maximum number of results to return (default 5)",
			},
			"tier_filter": {
				Type:        "array",
				Description: "Optional filter: 'curated', 'synced', 'derived'. Empty means search all tiers.",
				Items:       &llm.Property{Type: "string"},
			},
		},
		Required: []string{"query"},
	}
}

func (t *SearchKnowledgeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	topK := 5
	if v, ok := args["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}

	var tierFilter []knowledge.Tier
	if tf, ok := args["tier_filter"].([]any); ok {
		for _, t := range tf {
			if s, ok := t.(string); ok {
				tierFilter = append(tierFilter, knowledge.Tier(s))
			}
		}
	}

	results, err := t.client.SearchKnowledge(ctx, query, topK, tierFilter)
	if err != nil {
		return nil, fmt.Errorf("knowledge search failed: %w", err)
	}

	// Format results for LLM consumption.
	type resultItem struct {
		Title      string  `json:"title"`
		Content    string  `json:"content"`
		Tier       string  `json:"tier"`
		Type       string  `json:"type"`
		Similarity float32 `json:"similarity"`
		SourceURL  string  `json:"source_url,omitempty"`
		Confidence float64 `json:"confidence"`
	}

	items := make([]resultItem, 0, len(results))
	for _, r := range results {
		items = append(items, resultItem{
			Title:      r.Entry.Title,
			Content:    truncate(r.Entry.Content, 800),
			Tier:       string(r.Entry.Tier),
			Type:       string(r.Entry.Type),
			Similarity: r.Similarity,
			SourceURL:  r.Entry.SourceURL,
			Confidence: r.Entry.Confidence,
		})
	}

	return map[string]any{
		"results": items,
		"count":   len(items),
		"query":   query,
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Truncate at a word boundary.
	s = s[:max]
	if idx := strings.LastIndexAny(s, " \n\t"); idx > max/2 {
		s = s[:idx]
	}
	return s + "…"
}
