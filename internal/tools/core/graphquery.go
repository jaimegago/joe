package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
)

// GraphQueryClient is the interface for querying the infrastructure graph.
type GraphQueryClient interface {
	GraphQuery(ctx context.Context, query string) ([]graph.Node, error)
}

// GraphQueryTool queries the infrastructure graph for nodes.
type GraphQueryTool struct {
	client GraphQueryClient
}

// NewGraphQueryTool creates a new graph_query tool.
func NewGraphQueryTool(c GraphQueryClient) *GraphQueryTool {
	return &GraphQueryTool{client: c}
}

func (t *GraphQueryTool) Name() string { return "graph_query" }

func (t *GraphQueryTool) Description() string {
	return "Search the infrastructure graph for nodes. Use 'type:<type>' to filter by type (e.g. type:service, type:deployment) or free text to search by ID."
}

func (t *GraphQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"query": {
				Type:        "string",
				Description: "Search query. Use 'type:<type>' to filter by node type, or free text to search node IDs.",
			},
		},
		Required: []string{"query"},
	}
}

func (t *GraphQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	nodes, err := t.client.GraphQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("graph query failed: %w", err)
	}

	return map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	}, nil
}
