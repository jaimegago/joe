package core

import (
	"context"
	"fmt"
	"time"

	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	"github.com/jaimegago/joe/internal/llm"
)

// DatadogClient defines the subset of client.Client needed by DatadogQueryTool.
type DatadogClient interface {
	DatadogMetricsQuery(ctx context.Context, sourceID, query string, from, to int64) (*datadogadapter.MetricsResult, error)
	DatadogLogsSearch(ctx context.Context, sourceID, query string, from, to int64, limit int) (*datadogadapter.LogsResult, error)
}

// DatadogQueryTool queries Datadog metrics and logs via joecored.
type DatadogQueryTool struct {
	Client DatadogClient
}

// NewDatadogQueryTool creates a new datadog_query tool.
func NewDatadogQueryTool(c DatadogClient) *DatadogQueryTool {
	return &DatadogQueryTool{Client: c}
}

func (t *DatadogQueryTool) Name() string { return "datadog_query" }

func (t *DatadogQueryTool) Description() string {
	return "Query Datadog for metrics or log events. Supports metrics queries (Datadog query language) and log searches. If you don't know the source_id, call list_sources first."
}

func (t *DatadogQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Datadog source to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action to perform: 'metrics' (default) or 'logs'.",
			},
			"query": {
				Type:        "string",
				Description: "Metrics query string (e.g. 'avg:system.cpu.user{*}') or log filter query (e.g. 'service:payment status:error').",
			},
			"from": {
				Type:        "integer",
				Description: "Start Unix timestamp in seconds. Defaults to 1 hour ago.",
			},
			"to": {
				Type:        "integer",
				Description: "End Unix timestamp in seconds. Defaults to now.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of log events to return (default 25, max 1000). Only used for 'logs' action.",
			},
		},
		Required: []string{"source_id", "query"},
	}
}

func (t *DatadogQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "metrics"
	}

	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	if f, ok := args["from"].(float64); ok {
		from = int64(f)
	}
	if e, ok := args["to"].(float64); ok {
		to = int64(e)
	}

	switch action {
	case "logs":
		limit := 25
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		result, err := t.Client.DatadogLogsSearch(ctx, sourceID, query, from, to, limit)
		if err != nil {
			return nil, fmt.Errorf("datadog logs search failed: %w", err)
		}
		return map[string]any{
			"logs":      result.Logs,
			"count":     result.Count,
			"source_id": sourceID,
			"query":     query,
		}, nil

	default: // "metrics"
		result, err := t.Client.DatadogMetricsQuery(ctx, sourceID, query, from, to)
		if err != nil {
			return nil, fmt.Errorf("datadog metrics query failed: %w", err)
		}
		seriesCount := 0
		if result != nil {
			seriesCount = len(result.Series)
		}
		return map[string]any{
			"result":       result,
			"series_count": seriesCount,
			"source_id":    sourceID,
			"query":        query,
		}, nil
	}
}
