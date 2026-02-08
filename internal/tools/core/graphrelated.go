package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// GraphRelatedTool finds nodes related to a given node in the graph.
type GraphRelatedTool struct {
	client *client.Client
}

// NewGraphRelatedTool creates a new graph_related tool.
func NewGraphRelatedTool(c *client.Client) *GraphRelatedTool {
	return &GraphRelatedTool{client: c}
}

func (t *GraphRelatedTool) Name() string { return "graph_related" }

func (t *GraphRelatedTool) Description() string {
	return "Find nodes related to a given node in the infrastructure graph. Returns the subgraph of connected nodes and edges up to the specified depth."
}

func (t *GraphRelatedTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"node_id": {
				Type:        "string",
				Description: "The ID of the node to find related nodes for.",
			},
			"depth": {
				Type:        "integer",
				Description: "How many hops to traverse. Defaults to 1.",
			},
		},
		Required: []string{"node_id"},
	}
}

func (t *GraphRelatedTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	nodeID, ok := args["node_id"].(string)
	if !ok || nodeID == "" {
		return nil, fmt.Errorf("missing required parameter: node_id")
	}

	depth := 1
	if d, ok := args["depth"].(float64); ok {
		depth = int(d)
	}

	subgraph, err := t.client.GraphRelated(ctx, nodeID, depth)
	if err != nil {
		return nil, fmt.Errorf("graph related failed: %w", err)
	}

	return subgraph, nil
}
