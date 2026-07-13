package core

import (
	"context"
	"fmt"

	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/llm"
)

// TempoClient defines the subset of client.Client needed for TempoSearchTool.
type TempoClient interface {
	TempoSearch(ctx context.Context, sourceID, service, tags string, minDurationMs, maxDurationMs, limit int) ([]tempoadapter.TraceSearchResult, error)
	TempoGetTrace(ctx context.Context, sourceID, traceID string) (*tempoadapter.Trace, error)
}

// TempoSearchTool searches for distributed traces in Tempo via joecored.
type TempoSearchTool struct {
	Client TempoClient
}

// NewTempoSearchTool creates a new tempo_search tool.
func NewTempoSearchTool(c TempoClient) *TempoSearchTool {
	return &TempoSearchTool{Client: c}
}

func (t *TempoSearchTool) Name() string { return "tempo_search" }

func (t *TempoSearchTool) Description() string {
	return "Search for distributed traces in Tempo by service name, tags, or duration thresholds. Can also retrieve a full trace by ID to see the complete span tree. Useful for understanding latency, finding slow operations, and tracing request flows across services. If you don't know the component_id, call list_components first."
}

func (t *TempoSearchTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Tempo component to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action: 'search' (find traces, default) or 'get' (retrieve a specific trace by trace_id).",
			},
			"service": {
				Type:        "string",
				Description: "Filter by service name (e.g., 'payment', 'order-service').",
			},
			"tags": {
				Type:        "string",
				Description: "Additional tag filters in 'key=value' space-separated format (e.g., 'http.status_code=500').",
			},
			"min_duration": {
				Type:        "integer",
				Description: "Minimum trace duration in milliseconds. Use to find slow traces.",
			},
			"max_duration": {
				Type:        "integer",
				Description: "Maximum trace duration in milliseconds.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of traces to return. Defaults to 20.",
			},
			"trace_id": {
				Type:        "string",
				Description: "Trace ID for action 'get'.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *TempoSearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "search"
	}

	switch action {
	case "get":
		traceID, _ := args["trace_id"].(string)
		if traceID == "" {
			return nil, fmt.Errorf("trace_id is required for action 'get'")
		}

		trace, err := t.Client.TempoGetTrace(ctx, sourceID, traceID)
		if err != nil {
			return nil, fmt.Errorf("tempo get trace failed: %w", err)
		}
		return map[string]any{
			"trace":        trace,
			"component_id": sourceID,
		}, nil

	default: // "search"
		service, _ := args["service"].(string)
		tags, _ := args["tags"].(string)

		minDuration := 0
		if v, ok := args["min_duration"].(float64); ok {
			minDuration = int(v)
		}
		maxDuration := 0
		if v, ok := args["max_duration"].(float64); ok {
			maxDuration = int(v)
		}
		limit := 20
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		traces, err := t.Client.TempoSearch(ctx, sourceID, service, tags, minDuration, maxDuration, limit)
		if err != nil {
			return nil, fmt.Errorf("tempo search failed: %w", err)
		}
		return map[string]any{
			"traces":       traces,
			"count":        len(traces),
			"component_id": sourceID,
		}, nil
	}
}
