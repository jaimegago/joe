package core

import (
	"context"
	"fmt"
	"time"

	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	"github.com/jaimegago/joe/internal/llm"
)

// PrometheusClient defines the subset of client.Client needed for PrometheusQueryTool.
type PrometheusClient interface {
	PrometheusQuery(ctx context.Context, sourceID, query string, queryTime time.Time) (*prometheusadapter.QueryResult, error)
	PrometheusQueryRange(ctx context.Context, sourceID, query string, start, end time.Time, step time.Duration) (*prometheusadapter.QueryResult, error)
	PrometheusTargets(ctx context.Context, sourceID string) ([]prometheusadapter.Target, error)
}

// PrometheusQueryTool queries Prometheus/Mimir via joecored.
type PrometheusQueryTool struct {
	Client PrometheusClient
}

// NewPrometheusQueryTool creates a new prometheus_query tool.
func NewPrometheusQueryTool(c PrometheusClient) *PrometheusQueryTool {
	return &PrometheusQueryTool{Client: c}
}

func (t *PrometheusQueryTool) Name() string { return "prometheus_query" }

func (t *PrometheusQueryTool) Description() string {
	return "Query Prometheus or Mimir metrics using PromQL. Supports instant queries (current value) and range queries (over a time window). Also lists scrape targets to discover what services are being monitored. If you don't know the source_id, call list_sources first."
}

func (t *PrometheusQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Prometheus or Mimir source to query.",
			},
			"query": {
				Type:        "string",
				Description: "PromQL expression to evaluate. Omit when action is 'targets'.",
			},
			"action": {
				Type:        "string",
				Description: "Action to perform: 'query' (instant, default), 'query_range' (over time), or 'targets' (list scrape targets).",
			},
			"start": {
				Type:        "integer",
				Description: "Start Unix timestamp for query_range. Defaults to 1 hour ago.",
			},
			"end": {
				Type:        "integer",
				Description: "End Unix timestamp for query_range. Defaults to now.",
			},
			"step": {
				Type:        "integer",
				Description: "Step interval in seconds for query_range. Defaults to 15.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *PrometheusQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "query"
	}

	switch action {
	case "targets":
		targets, err := t.Client.PrometheusTargets(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("prometheus targets failed: %w", err)
		}
		return map[string]any{
			"targets":   targets,
			"count":     len(targets),
			"source_id": sourceID,
		}, nil

	case "query_range":
		query, _ := args["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("query is required for action 'query_range'")
		}

		now := time.Now()
		start := now.Add(-time.Hour)
		end := now
		step := 15 * time.Second

		if s, ok := args["start"].(float64); ok {
			start = time.Unix(int64(s), 0)
		}
		if e, ok := args["end"].(float64); ok {
			end = time.Unix(int64(e), 0)
		}
		if st, ok := args["step"].(float64); ok && st > 0 {
			step = time.Duration(int64(st)) * time.Second
		}

		result, err := t.Client.PrometheusQueryRange(ctx, sourceID, query, start, end, step)
		if err != nil {
			return nil, fmt.Errorf("prometheus query_range failed: %w", err)
		}
		return map[string]any{
			"result":    result,
			"source_id": sourceID,
			"query":     query,
		}, nil

	default: // "query"
		query, _ := args["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("query is required for action 'query'")
		}

		result, err := t.Client.PrometheusQuery(ctx, sourceID, query, time.Time{})
		if err != nil {
			return nil, fmt.Errorf("prometheus query failed: %w", err)
		}
		return map[string]any{
			"result":    result,
			"source_id": sourceID,
			"query":     query,
		}, nil
	}
}
