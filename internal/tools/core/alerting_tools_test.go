package core

import (
	"context"
	"errors"
	"testing"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
)

// --- Mock clients ---

type mockAlertmanagerClient struct {
	alerts []alertmanageradapter.Alert
	err    error
}

func (m *mockAlertmanagerClient) AlertmanagerAlerts(_ context.Context, _, _ string) ([]alertmanageradapter.Alert, error) {
	return m.alerts, m.err
}

type mockPagerDutyClient struct {
	incidents []pagerdutyadapter.Incident
	services  []pagerdutyadapter.Service
	err       error
}

func (m *mockPagerDutyClient) PagerDutyIncidents(_ context.Context, _, _, _ string, _ int) ([]pagerdutyadapter.Incident, error) {
	return m.incidents, m.err
}

func (m *mockPagerDutyClient) PagerDutyServices(_ context.Context, _ string) ([]pagerdutyadapter.Service, error) {
	return m.services, m.err
}

type mockGrafanaClient struct {
	dashboards []grafanaadapter.Dashboard
	dashboard  *grafanaadapter.DashboardDetail
	alerts     []grafanaadapter.GrafanaAlert
	err        error
}

func (m *mockGrafanaClient) GrafanaDashboards(_ context.Context, _, _ string, _ int) ([]grafanaadapter.Dashboard, error) {
	return m.dashboards, m.err
}

func (m *mockGrafanaClient) GrafanaDashboard(_ context.Context, _, _ string) (*grafanaadapter.DashboardDetail, error) {
	return m.dashboard, m.err
}

func (m *mockGrafanaClient) GrafanaAlerts(_ context.Context, _ string) ([]grafanaadapter.GrafanaAlert, error) {
	return m.alerts, m.err
}

// --- Metadata tests ---

func TestAlertmanagerAlertsTool_Metadata(t *testing.T) {
	tool := NewAlertmanagerAlertsTool(&mockAlertmanagerClient{})
	if tool.Name() != "alertmanager_alerts" {
		t.Errorf("Name() = %q, want alertmanager_alerts", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestPagerDutyIncidentsTool_Metadata(t *testing.T) {
	tool := NewPagerDutyIncidentsTool(&mockPagerDutyClient{})
	if tool.Name() != "pagerduty_incidents" {
		t.Errorf("Name() = %q, want pagerduty_incidents", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestGrafanaDashboardsTool_Metadata(t *testing.T) {
	tool := NewGrafanaDashboardsTool(&mockGrafanaClient{})
	if tool.Name() != "grafana_dashboards" {
		t.Errorf("Name() = %q, want grafana_dashboards", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

// --- AlertmanagerAlertsTool tests ---

func TestAlertmanagerAlertsTool_Execute(t *testing.T) {
	sampleAlerts := []alertmanageradapter.Alert{
		{
			Fingerprint: "abc123",
			Status:      alertmanageradapter.AlertStatus{State: "active"},
			Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
		},
	}

	tests := []struct {
		name    string
		args    map[string]any
		client  *mockAlertmanagerClient
		wantN   int
		wantErr bool
	}{
		{
			name:   "list alerts",
			args:   map[string]any{"component_id": "am-prod"},
			client: &mockAlertmanagerClient{alerts: sampleAlerts},
			wantN:  1,
		},
		{
			name:   "list alerts with filter",
			args:   map[string]any{"component_id": "am-prod", "filter": "severity=critical"},
			client: &mockAlertmanagerClient{alerts: sampleAlerts},
			wantN:  1,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockAlertmanagerClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "am-prod"},
			client:  &mockAlertmanagerClient{err: errors.New("connection refused")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewAlertmanagerAlertsTool(tt.client)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				alerts := m["alerts"].([]alertmanageradapter.Alert)
				if len(alerts) != tt.wantN {
					t.Errorf("len(alerts) = %d, want %d", len(alerts), tt.wantN)
				}
			}
		})
	}
}

// --- PagerDutyIncidentsTool tests ---

func TestPagerDutyIncidentsTool_Execute(t *testing.T) {
	sampleIncidents := []pagerdutyadapter.Incident{
		{ID: "INC001", Title: "Payment down", Status: "triggered"},
	}
	sampleServices := []pagerdutyadapter.Service{
		{ID: "SVC001", Name: "payment"},
	}

	tests := []struct {
		name    string
		args    map[string]any
		client  *mockPagerDutyClient
		wantKey string
		wantN   int
		wantErr bool
	}{
		{
			name:    "list incidents (default)",
			args:    map[string]any{"component_id": "pd-prod"},
			client:  &mockPagerDutyClient{incidents: sampleIncidents},
			wantKey: "incidents",
			wantN:   1,
		},
		{
			name:    "list services",
			args:    map[string]any{"component_id": "pd-prod", "action": "services"},
			client:  &mockPagerDutyClient{services: sampleServices},
			wantKey: "services",
			wantN:   1,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockPagerDutyClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "pd-prod"},
			client:  &mockPagerDutyClient{err: errors.New("unauthorized")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewPagerDutyIncidentsTool(tt.client)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				switch tt.wantKey {
				case "incidents":
					items := m["incidents"].([]pagerdutyadapter.Incident)
					if len(items) != tt.wantN {
						t.Errorf("len(incidents) = %d, want %d", len(items), tt.wantN)
					}
				case "services":
					items := m["services"].([]pagerdutyadapter.Service)
					if len(items) != tt.wantN {
						t.Errorf("len(services) = %d, want %d", len(items), tt.wantN)
					}
				}
			}
		})
	}
}

// --- GrafanaDashboardsTool tests ---

func TestGrafanaDashboardsTool_Execute(t *testing.T) {
	sampleDashboards := []grafanaadapter.Dashboard{
		{ID: 1, UID: "abc123", Title: "Payment Overview"},
	}
	sampleDetail := &grafanaadapter.DashboardDetail{
		ID:    1,
		UID:   "abc123",
		Title: "Payment Overview",
		Panels: []grafanaadapter.Panel{
			{ID: 1, Title: "Request Rate", Type: "graph"},
		},
	}
	sampleAlerts := []grafanaadapter.GrafanaAlert{
		{Fingerprint: "gf001", State: "active", Labels: map[string]string{"alertname": "HighLatency"}},
	}

	tests := []struct {
		name    string
		args    map[string]any
		client  *mockGrafanaClient
		wantKey string
		wantN   int
		wantErr bool
	}{
		{
			name:    "search dashboards (default)",
			args:    map[string]any{"component_id": "grafana-prod"},
			client:  &mockGrafanaClient{dashboards: sampleDashboards},
			wantKey: "dashboards",
			wantN:   1,
		},
		{
			name:    "get dashboard by uid",
			args:    map[string]any{"component_id": "grafana-prod", "action": "get", "uid": "abc123"},
			client:  &mockGrafanaClient{dashboard: sampleDetail},
			wantKey: "dashboard",
		},
		{
			name:    "list alerts",
			args:    map[string]any{"component_id": "grafana-prod", "action": "alerts"},
			client:  &mockGrafanaClient{alerts: sampleAlerts},
			wantKey: "alerts",
			wantN:   1,
		},
		{
			name:    "get without uid",
			args:    map[string]any{"component_id": "grafana-prod", "action": "get"},
			client:  &mockGrafanaClient{},
			wantErr: true,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockGrafanaClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "grafana-prod"},
			client:  &mockGrafanaClient{err: errors.New("timeout")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewGrafanaDashboardsTool(tt.client)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				switch tt.wantKey {
				case "dashboards":
					items := m["dashboards"].([]grafanaadapter.Dashboard)
					if len(items) != tt.wantN {
						t.Errorf("len(dashboards) = %d, want %d", len(items), tt.wantN)
					}
				case "dashboard":
					if m["dashboard"] == nil {
						t.Error("dashboard is nil")
					}
				case "alerts":
					items := m["alerts"].([]grafanaadapter.GrafanaAlert)
					if len(items) != tt.wantN {
						t.Errorf("len(alerts) = %d, want %d", len(items), tt.wantN)
					}
				}
			}
		})
	}
}
