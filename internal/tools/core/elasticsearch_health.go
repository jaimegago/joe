package core

import (
	"context"
	"fmt"

	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	"github.com/jaimegago/joe/internal/llm"
)

// ElasticsearchClient defines the subset of client.Client needed for Elasticsearch tools.
type ElasticsearchClient interface {
	ElasticsearchHealth(ctx context.Context, sourceID string) (*elasticsearchadapter.ClusterHealth, error)
	ElasticsearchIndices(ctx context.Context, sourceID, pattern string) ([]elasticsearchadapter.IndexInfo, error)
}

// ElasticsearchHealthTool retrieves Elasticsearch cluster health via joecored.
type ElasticsearchHealthTool struct {
	Client ElasticsearchClient
}

// NewElasticsearchHealthTool creates a new elasticsearch_health tool.
func NewElasticsearchHealthTool(c ElasticsearchClient) *ElasticsearchHealthTool {
	return &ElasticsearchHealthTool{Client: c}
}

func (t *ElasticsearchHealthTool) Name() string { return "elasticsearch_health" }

func (t *ElasticsearchHealthTool) Description() string {
	return "Retrieve Elasticsearch cluster health including status (green/yellow/red), " +
		"node counts, active shards, unassigned shards, and relocating shards. " +
		"Use this to quickly assess cluster health and identify shard allocation problems. " +
		"If you don't know the component_id, call list_components first."
}

func (t *ElasticsearchHealthTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Elasticsearch component to query.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *ElasticsearchHealthTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	health, err := t.Client.ElasticsearchHealth(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch health: %w", err)
	}

	return map[string]any{
		"health":       health,
		"component_id": sourceID,
	}, nil
}

// ElasticsearchIndicesTool retrieves Elasticsearch index information via joecored.
type ElasticsearchIndicesTool struct {
	Client ElasticsearchClient
}

// NewElasticsearchIndicesTool creates a new elasticsearch_indices tool.
func NewElasticsearchIndicesTool(c ElasticsearchClient) *ElasticsearchIndicesTool {
	return &ElasticsearchIndicesTool{Client: c}
}

func (t *ElasticsearchIndicesTool) Name() string { return "elasticsearch_indices" }

func (t *ElasticsearchIndicesTool) Description() string {
	return "List Elasticsearch indices with health, status, document count, store size, and shard counts. " +
		"Supports an optional wildcard pattern to filter indices (e.g. 'logs-*', 'metrics-*'). " +
		"Use this to identify unhealthy indices, large indices, and storage usage. " +
		"If you don't know the component_id, call list_components first."
}

func (t *ElasticsearchIndicesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Elasticsearch component to query.",
			},
			"pattern": {
				Type:        "string",
				Description: "Optional wildcard pattern to filter indices (e.g. 'logs-*'). Omit for all indices.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *ElasticsearchIndicesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	pattern, _ := args["pattern"].(string)

	indices, err := t.Client.ElasticsearchIndices(ctx, sourceID, pattern)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch indices: %w", err)
	}

	if indices == nil {
		indices = []elasticsearchadapter.IndexInfo{}
	}

	return map[string]any{
		"indices":      indices,
		"count":        len(indices),
		"component_id": sourceID,
		"pattern":      pattern,
	}, nil
}
