package core

import (
	"context"
	"fmt"

	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	"github.com/jaimegago/joe/internal/llm"
)

// SplunkClient defines the subset of client.Client needed by SplunkQueryTool.
type SplunkClient interface {
	SplunkSearch(ctx context.Context, sourceID, query, earliest, latest string, limit int) (*splunkadapter.SearchResult, error)
}

// SplunkQueryTool executes Splunk SPL searches via joecored.
type SplunkQueryTool struct {
	Client SplunkClient
}

// NewSplunkQueryTool creates a new splunk_query tool.
func NewSplunkQueryTool(c SplunkClient) *SplunkQueryTool {
	return &SplunkQueryTool{Client: c}
}

func (t *SplunkQueryTool) Name() string { return "splunk_query" }

func (t *SplunkQueryTool) Description() string {
	return "Search Splunk logs using SPL (Splunk Processing Language). Returns log events matching the query within the specified time range. If you don't know the component_id, call list_components first."
}

func (t *SplunkQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Splunk source to query.",
			},
			"query": {
				Type:        "string",
				Description: "SPL query expression (e.g. 'index=main error | stats count by host'). The 'search' prefix is added automatically if omitted.",
			},
			"earliest": {
				Type:        "string",
				Description: "Earliest time boundary. Accepts Splunk relative modifiers (e.g. '-1h', '-15m', '-1d') or ISO-8601 timestamps. Defaults to '-1h'.",
			},
			"latest": {
				Type:        "string",
				Description: "Latest time boundary. Defaults to 'now'.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of events to return (default 100).",
			},
		},
		Required: []string{"component_id", "query"},
	}
}

func (t *SplunkQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	earliest, _ := args["earliest"].(string)
	if earliest == "" {
		earliest = "-1h"
	}

	latest, _ := args["latest"].(string)
	if latest == "" {
		latest = "now"
	}

	limit := 100
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	result, err := t.Client.SplunkSearch(ctx, sourceID, query, earliest, latest, limit)
	if err != nil {
		return nil, fmt.Errorf("splunk search failed: %w", err)
	}

	return map[string]any{
		"events":       result.Events,
		"count":        result.Count,
		"component_id": sourceID,
		"query":        query,
	}, nil
}
