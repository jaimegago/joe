package core

import (
	"context"
	"fmt"

	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/llm"
)

// PagerDutyClient defines the subset of client.Client needed for PagerDutyIncidentsTool.
type PagerDutyClient interface {
	PagerDutyIncidents(ctx context.Context, sourceID, serviceID, status string, limit int) ([]pagerdutyadapter.Incident, error)
	PagerDutyServices(ctx context.Context, sourceID string) ([]pagerdutyadapter.Service, error)
}

// PagerDutyIncidentsTool queries PagerDuty incidents and services via joecored.
type PagerDutyIncidentsTool struct {
	Client PagerDutyClient
}

// NewPagerDutyIncidentsTool creates a new pagerduty_incidents tool.
func NewPagerDutyIncidentsTool(c PagerDutyClient) *PagerDutyIncidentsTool {
	return &PagerDutyIncidentsTool{Client: c}
}

func (t *PagerDutyIncidentsTool) Name() string { return "pagerduty_incidents" }

func (t *PagerDutyIncidentsTool) Description() string {
	return "Query PagerDuty incidents and services. List active incidents (triggered or acknowledged), filter by service or status, or list all services. Useful for understanding what's currently paging and who is on-call. If you don't know the component_id, call list_components first."
}

func (t *PagerDutyIncidentsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the PagerDuty component to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action: 'incidents' (list incidents, default) or 'services' (list all PagerDuty services).",
			},
			"service": {
				Type:        "string",
				Description: "Filter incidents by PagerDuty service ID. Optional.",
			},
			"status": {
				Type:        "string",
				Description: "Filter by status: 'triggered', 'acknowledged', 'resolved', or comma-separated. Defaults to triggered+acknowledged.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of incidents to return. Defaults to 25.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *PagerDutyIncidentsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "incidents"
	}

	switch action {
	case "services":
		services, err := t.Client.PagerDutyServices(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("pagerduty list services failed: %w", err)
		}
		return map[string]any{
			"services":     services,
			"count":        len(services),
			"component_id": sourceID,
		}, nil

	default: // "incidents"
		serviceID, _ := args["service"].(string)
		status, _ := args["status"].(string)

		limit := 25
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		incidents, err := t.Client.PagerDutyIncidents(ctx, sourceID, serviceID, status, limit)
		if err != nil {
			return nil, fmt.Errorf("pagerduty list incidents failed: %w", err)
		}
		return map[string]any{
			"incidents":    incidents,
			"count":        len(incidents),
			"component_id": sourceID,
		}, nil
	}
}
