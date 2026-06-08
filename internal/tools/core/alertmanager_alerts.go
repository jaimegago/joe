package core

import (
	"context"
	"fmt"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	"github.com/jaimegago/joe/internal/llm"
)

// AlertmanagerClient defines the subset of client.Client needed for AlertmanagerAlertsTool.
type AlertmanagerClient interface {
	AlertmanagerAlerts(ctx context.Context, sourceID, filter string) ([]alertmanageradapter.Alert, error)
}

// AlertmanagerAlertsTool queries active alerts from Alertmanager via joecored.
type AlertmanagerAlertsTool struct {
	Client AlertmanagerClient
}

// NewAlertmanagerAlertsTool creates a new alertmanager_alerts tool.
func NewAlertmanagerAlertsTool(c AlertmanagerClient) *AlertmanagerAlertsTool {
	return &AlertmanagerAlertsTool{Client: c}
}

func (t *AlertmanagerAlertsTool) Name() string { return "alertmanager_alerts" }

func (t *AlertmanagerAlertsTool) Description() string {
	return "List active alerts from Alertmanager. Optionally filter by label matchers (e.g., \"severity=critical\" or \"service=payment\"). Returns alert details including state, labels, annotations, and receivers. If you don't know the component_id, call list_components first."
}

func (t *AlertmanagerAlertsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Alertmanager source to query.",
			},
			"filter": {
				Type:        "string",
				Description: "Optional label matcher to filter alerts (e.g., \"severity=critical\", \"alertname=HighCPU\").",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *AlertmanagerAlertsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	filter, _ := args["filter"].(string)

	alerts, err := t.Client.AlertmanagerAlerts(ctx, sourceID, filter)
	if err != nil {
		return nil, fmt.Errorf("alertmanager alerts failed: %w", err)
	}

	return map[string]any{
		"alerts":       alerts,
		"count":        len(alerts),
		"component_id": sourceID,
	}, nil
}
