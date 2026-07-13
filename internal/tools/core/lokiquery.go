package core

import (
	"context"
	"fmt"
	"time"

	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	"github.com/jaimegago/joe/internal/llm"
)

// LokiClient defines the subset of client.Client needed for LokiQueryTool.
type LokiClient interface {
	LokiQuery(ctx context.Context, sourceID, query string, limit int, since time.Duration) (*lokiadapter.QueryResult, error)
	LokiQueryRange(ctx context.Context, sourceID, query string, start, end time.Time, limit int) (*lokiadapter.QueryResult, error)
}

// LokiQueryTool queries Loki log aggregation via joecored.
type LokiQueryTool struct {
	Client LokiClient
}

// NewLokiQueryTool creates a new loki_query tool.
func NewLokiQueryTool(c LokiClient) *LokiQueryTool {
	return &LokiQueryTool{Client: c}
}

func (t *LokiQueryTool) Name() string { return "loki_query" }

func (t *LokiQueryTool) Description() string {
	return "Query Loki logs using LogQL. Supports instant queries (tail from now) and range queries over a time window. Use this to search for errors, trace request flows, or correlate logs with incidents. If you don't know the component_id, call list_components first."
}

func (t *LokiQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Loki component to query.",
			},
			"query": {
				Type:        "string",
				Description: "LogQL expression. Examples: '{app=\"payment\"}', '{namespace=\"prod\"} |= \"error\"'.",
			},
			"action": {
				Type:        "string",
				Description: "Action: 'query' (instant tail, default) or 'query_range' (over a time window).",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of log lines to return. Defaults to 100.",
			},
			"since": {
				Type:        "integer",
				Description: "For 'query': how many seconds back to look. Defaults to 3600 (1 hour).",
			},
			"start": {
				Type:        "integer",
				Description: "For 'query_range': start Unix timestamp.",
			},
			"end": {
				Type:        "integer",
				Description: "For 'query_range': end Unix timestamp. Defaults to now.",
			},
		},
		Required: []string{"component_id", "query"},
	}
}

func (t *LokiQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "query"
	}

	limit := 100
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	switch action {
	case "query_range":
		now := time.Now()
		start := now.Add(-time.Hour)
		end := now

		if s, ok := args["start"].(float64); ok {
			start = time.Unix(int64(s), 0)
		}
		if e, ok := args["end"].(float64); ok {
			end = time.Unix(int64(e), 0)
		}

		result, err := t.Client.LokiQueryRange(ctx, sourceID, query, start, end, limit)
		if err != nil {
			return nil, fmt.Errorf("loki query_range failed: %w", err)
		}
		return map[string]any{
			"result":       result,
			"component_id": sourceID,
			"query":        query,
		}, nil

	default: // "query"
		since := time.Hour
		if s, ok := args["since"].(float64); ok && s > 0 {
			since = time.Duration(int64(s)) * time.Second
		}

		result, err := t.Client.LokiQuery(ctx, sourceID, query, limit, since)
		if err != nil {
			return nil, fmt.Errorf("loki query failed: %w", err)
		}
		return map[string]any{
			"result":       result,
			"component_id": sourceID,
			"query":        query,
		}, nil
	}
}
