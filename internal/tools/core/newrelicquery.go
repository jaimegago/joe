package core

import (
	"context"
	"fmt"

	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	"github.com/jaimegago/joe/internal/llm"
)

// NewRelicClient defines the subset of client.Client needed by NewRelicQueryTool.
type NewRelicClient interface {
	NewRelicNRQLQuery(ctx context.Context, sourceID string, accountID int, query string) (*newrelicadapter.NRQLResult, error)
}

// NewRelicQueryTool executes New Relic NRQL queries via joecored.
type NewRelicQueryTool struct {
	Client NewRelicClient
}

// NewNewRelicQueryTool creates a new newrelic_query tool.
func NewNewRelicQueryTool(c NewRelicClient) *NewRelicQueryTool {
	return &NewRelicQueryTool{Client: c}
}

func (t *NewRelicQueryTool) Name() string { return "newrelic_query" }

func (t *NewRelicQueryTool) Description() string {
	return "Execute NRQL (New Relic Query Language) queries against New Relic. Returns telemetry data including metrics, events, logs, and traces. If you don't know the source_id, call list_sources first."
}

func (t *NewRelicQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the New Relic source to query.",
			},
			"query": {
				Type:        "string",
				Description: "NRQL query (e.g. 'SELECT count(*) FROM Transaction SINCE 1 hour ago FACET appName').",
			},
			"account_id": {
				Type:        "integer",
				Description: "New Relic account ID to query. Defaults to the account configured on the source.",
			},
		},
		Required: []string{"source_id", "query"},
	}
}

func (t *NewRelicQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	accountID := 0
	if id, ok := args["account_id"].(float64); ok {
		accountID = int(id)
	}

	result, err := t.Client.NewRelicNRQLQuery(ctx, sourceID, accountID, query)
	if err != nil {
		return nil, fmt.Errorf("new relic nrql query failed: %w", err)
	}

	return map[string]any{
		"results":   result.Results,
		"metadata":  result.Metadata,
		"count":     len(result.Results),
		"source_id": sourceID,
		"query":     query,
	}, nil
}
