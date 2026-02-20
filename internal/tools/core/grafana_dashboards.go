package core

import (
	"context"
	"fmt"

	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	"github.com/jaimegago/joe/internal/llm"
)

// GrafanaClient defines the subset of client.Client needed for GrafanaDashboardsTool.
type GrafanaClient interface {
	GrafanaDashboards(ctx context.Context, sourceID, query string, limit int) ([]grafanaadapter.Dashboard, error)
	GrafanaDashboard(ctx context.Context, sourceID, uid string) (*grafanaadapter.DashboardDetail, error)
	GrafanaAlerts(ctx context.Context, sourceID string) ([]grafanaadapter.GrafanaAlert, error)
}

// GrafanaDashboardsTool queries Grafana dashboards and alerts via joecored.
type GrafanaDashboardsTool struct {
	Client GrafanaClient
}

// NewGrafanaDashboardsTool creates a new grafana_dashboards tool.
func NewGrafanaDashboardsTool(c GrafanaClient) *GrafanaDashboardsTool {
	return &GrafanaDashboardsTool{Client: c}
}

func (t *GrafanaDashboardsTool) Name() string { return "grafana_dashboards" }

func (t *GrafanaDashboardsTool) Description() string {
	return "Query Grafana dashboards and alerts. Search for dashboards by keyword, retrieve a specific dashboard with its panels, or list active Grafana-managed alerts. Useful for finding the right dashboard for a service and understanding current alert state. If you don't know the source_id, call list_sources first."
}

func (t *GrafanaDashboardsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Grafana source to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action: 'search' (list dashboards, default), 'get' (get dashboard by UID), or 'alerts' (list active alerts).",
			},
			"query": {
				Type:        "string",
				Description: "Search query for filtering dashboards by title or tag. Used with action 'search'.",
			},
			"uid": {
				Type:        "string",
				Description: "Dashboard UID for retrieving a specific dashboard. Required for action 'get'.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of dashboards to return. Defaults to 50.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *GrafanaDashboardsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "search"
	}

	switch action {
	case "get":
		uid, _ := args["uid"].(string)
		if uid == "" {
			return nil, fmt.Errorf("uid is required for action 'get'")
		}

		dashboard, err := t.Client.GrafanaDashboard(ctx, sourceID, uid)
		if err != nil {
			return nil, fmt.Errorf("grafana get dashboard failed: %w", err)
		}
		return map[string]any{
			"dashboard": dashboard,
			"source_id": sourceID,
		}, nil

	case "alerts":
		alerts, err := t.Client.GrafanaAlerts(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("grafana list alerts failed: %w", err)
		}
		return map[string]any{
			"alerts":    alerts,
			"count":     len(alerts),
			"source_id": sourceID,
		}, nil

	default: // "search"
		query, _ := args["query"].(string)
		limit := 50
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		dashboards, err := t.Client.GrafanaDashboards(ctx, sourceID, query, limit)
		if err != nil {
			return nil, fmt.Errorf("grafana search dashboards failed: %w", err)
		}
		return map[string]any{
			"dashboards": dashboards,
			"count":      len(dashboards),
			"source_id":  sourceID,
		}, nil
	}
}
