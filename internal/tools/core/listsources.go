package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// ListSourcesTool queries all registered infrastructure sources from joecored.
type ListSourcesTool struct {
	client *client.Client
}

// NewListSourcesTool creates a new list_sources tool.
func NewListSourcesTool(c *client.Client) *ListSourcesTool {
	return &ListSourcesTool{client: c}
}

func (t *ListSourcesTool) Name() string { return "list_sources" }

func (t *ListSourcesTool) Description() string {
	return "List all registered infrastructure sources (Kubernetes clusters, Git repositories, etc.). Returns source IDs, types, and names. Use this to discover available source_id values for other tools."
}

func (t *ListSourcesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"type": {
				Type:        "string",
				Description: "Optional: filter sources by type (kubernetes, git). Omit to list all sources.",
			},
		},
		Required: []string{},
	}
}

func (t *ListSourcesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sources, err := t.client.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources failed: %w", err)
	}

	// Optional type filter
	typeFilter, _ := args["type"].(string)
	if typeFilter != "" {
		filtered := make([]map[string]any, 0)
		for _, src := range sources {
			if src.Type == typeFilter {
				filtered = append(filtered, map[string]any{
					"id":   src.ID,
					"type": src.Type,
					"name": src.Name,
				})
			}
		}
		return map[string]any{
			"sources": filtered,
			"count":   len(filtered),
			"type":    typeFilter,
		}, nil
	}

	// Return all sources
	result := make([]map[string]any, len(sources))
	for i, src := range sources {
		result[i] = map[string]any{
			"id":   src.ID,
			"type": src.Type,
			"name": src.Name,
		}
	}

	return map[string]any{
		"sources": result,
		"count":   len(sources),
	}, nil
}
