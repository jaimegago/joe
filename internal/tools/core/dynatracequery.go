package core

import (
	"context"
	"fmt"
	"time"

	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	"github.com/jaimegago/joe/internal/llm"
)

// DynatraceClient defines the subset of client.Client needed by DynatraceQueryTool.
type DynatraceClient interface {
	DynatraceMetricsQuery(ctx context.Context, sourceID, query string, from, to int64) (*dynatraceadapter.MetricsResult, error)
	DynatraceEvents(ctx context.Context, sourceID string, from, to int64, limit int) (*dynatraceadapter.EventsResult, error)
}

// DynatraceQueryTool queries Dynatrace metrics and events via joecored.
type DynatraceQueryTool struct {
	Client DynatraceClient
}

// NewDynatraceQueryTool creates a new dynatrace_query tool.
func NewDynatraceQueryTool(c DynatraceClient) *DynatraceQueryTool {
	return &DynatraceQueryTool{Client: c}
}

func (t *DynatraceQueryTool) Name() string { return "dynatrace_query" }

func (t *DynatraceQueryTool) Description() string {
	return "Query Dynatrace for metrics or events. Supports metrics selector queries (DQL-style) and event feed queries. If you don't know the source_id, call list_sources first."
}

func (t *DynatraceQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Dynatrace source to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action to perform: 'metrics' (default) or 'events'.",
			},
			"query": {
				Type:        "string",
				Description: "Metric selector expression for 'metrics' action (e.g. 'builtin:host.cpu.usage:avg'). Not used for 'events'.",
			},
			"from": {
				Type:        "integer",
				Description: "Start time in Unix milliseconds. Defaults to 1 hour ago.",
			},
			"to": {
				Type:        "integer",
				Description: "End time in Unix milliseconds. Defaults to now.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of events to return (default 50). Only used for 'events' action.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *DynatraceQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "metrics"
	}

	now := time.Now()
	from := now.Add(-time.Hour).UnixMilli()
	to := now.UnixMilli()

	if f, ok := args["from"].(float64); ok {
		from = int64(f)
	}
	if e, ok := args["to"].(float64); ok {
		to = int64(e)
	}

	switch action {
	case "events":
		limit := 50
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		result, err := t.Client.DynatraceEvents(ctx, sourceID, from, to, limit)
		if err != nil {
			return nil, fmt.Errorf("dynatrace events query failed: %w", err)
		}
		return map[string]any{
			"events":    result.Events,
			"count":     result.Count,
			"source_id": sourceID,
		}, nil

	default: // "metrics"
		query, _ := args["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("query is required for action 'metrics'")
		}

		result, err := t.Client.DynatraceMetricsQuery(ctx, sourceID, query, from, to)
		if err != nil {
			return nil, fmt.Errorf("dynatrace metrics query failed: %w", err)
		}
		return map[string]any{
			"result":    result,
			"source_id": sourceID,
			"query":     query,
		}, nil
	}
}
