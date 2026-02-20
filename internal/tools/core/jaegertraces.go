package core

import (
	"context"
	"fmt"

	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	"github.com/jaimegago/joe/internal/llm"
)

// JaegerClient defines the subset of client.Client needed for JaegerTracesTool.
type JaegerClient interface {
	JaegerServices(ctx context.Context, sourceID string) ([]string, error)
	JaegerTraces(ctx context.Context, sourceID, service, operation string, limit int) ([]jaegeradapter.TraceSearchResult, error)
}

// JaegerTracesTool queries distributed traces from Jaeger via joecored.
type JaegerTracesTool struct {
	Client JaegerClient
}

// NewJaegerTracesTool creates a new jaeger_traces tool.
func NewJaegerTracesTool(c JaegerClient) *JaegerTracesTool {
	return &JaegerTracesTool{Client: c}
}

func (t *JaegerTracesTool) Name() string { return "jaeger_traces" }

func (t *JaegerTracesTool) Description() string {
	return "Query distributed traces from Jaeger. List all services Jaeger knows about, or search for traces by service name and operation. Useful for understanding request flows, finding latency issues, and debugging distributed systems. If you don't know the source_id, call list_sources first."
}

func (t *JaegerTracesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Jaeger source to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action: 'services' (list all services) or 'traces' (search for traces, default).",
			},
			"service": {
				Type:        "string",
				Description: "Service name to search traces for. Required for action 'traces'.",
			},
			"operation": {
				Type:        "string",
				Description: "Filter by operation name (e.g., 'POST /checkout'). Optional.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of traces to return. Defaults to 20.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *JaegerTracesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "traces"
	}

	switch action {
	case "services":
		services, err := t.Client.JaegerServices(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("jaeger list services failed: %w", err)
		}
		return map[string]any{
			"services":  services,
			"count":     len(services),
			"source_id": sourceID,
		}, nil

	default: // "traces"
		service, _ := args["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("service is required for action 'traces'")
		}

		operation, _ := args["operation"].(string)

		limit := 20
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		traces, err := t.Client.JaegerTraces(ctx, sourceID, service, operation, limit)
		if err != nil {
			return nil, fmt.Errorf("jaeger search traces failed: %w", err)
		}
		return map[string]any{
			"traces":    traces,
			"count":     len(traces),
			"source_id": sourceID,
		}, nil
	}
}
