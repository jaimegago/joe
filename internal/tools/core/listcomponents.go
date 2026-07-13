package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// ListComponentsClient defines the subset of client.Client needed for ListComponentsTool.
type ListComponentsClient interface {
	ListComponents(ctx context.Context) ([]*store.Component, error)
}

// ListComponentsTool queries all registered infrastructure components from joecored.
type ListComponentsTool struct {
	client ListComponentsClient
}

// NewListComponentsTool creates a new list_components tool.
func NewListComponentsTool(c ListComponentsClient) *ListComponentsTool {
	return &ListComponentsTool{client: c}
}

func (t *ListComponentsTool) Name() string { return "list_components" }

func (t *ListComponentsTool) Description() string {
	return "List all registered infrastructure components (Kubernetes clusters, Git repositories, etc.). Returns component IDs, types, and names. Use this to discover available component_id values for other tools."
}

func (t *ListComponentsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"type": {
				Type:        "string",
				Description: "Optional: filter components by type (kubernetes, git). Omit to list all components.",
			},
		},
		Required: []string{},
	}
}

func (t *ListComponentsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	components, err := t.client.ListComponents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list components failed: %w", err)
	}

	// Optional type filter
	typeFilter, _ := args["type"].(string)
	if typeFilter != "" {
		filtered := make([]map[string]any, 0)
		for _, src := range components {
			if src.Type == typeFilter {
				filtered = append(filtered, map[string]any{
					"id":   src.ID,
					"type": src.Type,
					"name": src.Name,
				})
			}
		}
		return map[string]any{
			"components": filtered,
			"count":      len(filtered),
			"type":       typeFilter,
		}, nil
	}

	// Return all components
	result := make([]map[string]any, len(components))
	for i, src := range components {
		result[i] = map[string]any{
			"id":   src.ID,
			"type": src.Type,
			"name": src.Name,
		}
	}

	return map[string]any{
		"components": result,
		"count":      len(components),
	}, nil
}
