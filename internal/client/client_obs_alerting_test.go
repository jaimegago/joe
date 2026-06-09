package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
)

func TestAlertingClientEndpoints(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/alertmanager/src/alerts"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"alerts":       []alertmanageradapter.Alert{{Fingerprint: "fp1"}},
				"count":        1,
				"component_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/pagerduty/src/incidents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"incidents":    []pagerdutyadapter.Incident{{ID: "INC001"}},
				"count":        1,
				"component_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/pagerduty/src/services"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"services":     []pagerdutyadapter.Service{{ID: "SVC001", Name: "payment"}},
				"count":        1,
				"component_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/grafana/src/dashboards") && !strings.Contains(r.URL.Path, "/dashboards/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dashboards":   []grafanaadapter.Dashboard{{UID: "dash-1", Title: "Overview"}},
				"count":        1,
				"component_id": "src",
			})
		case strings.Contains(r.URL.Path, "/grafana/src/dashboards/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dashboard":    &grafanaadapter.DashboardDetail{UID: "dash-1", Title: "Overview"},
				"component_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/grafana/src/alerts"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"alerts":       []grafanaadapter.GrafanaAlert{{Fingerprint: "fp2", State: "active"}},
				"count":        1,
				"component_id": "src",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)

	// AlertmanagerAlerts
	alerts, err := c.AlertmanagerAlerts(context.Background(), "src", "")
	if err != nil {
		t.Fatalf("AlertmanagerAlerts() error: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Fingerprint != "fp1" {
		t.Errorf("AlertmanagerAlerts: got %v", alerts)
	}

	// AlertmanagerAlerts with filter (tests the filter query param branch)
	_, _ = c.AlertmanagerAlerts(context.Background(), "src", "severity=critical")

	// PagerDutyIncidents
	incidents, err := c.PagerDutyIncidents(context.Background(), "src", "", "", 10)
	if err != nil {
		t.Fatalf("PagerDutyIncidents() error: %v", err)
	}
	if len(incidents) != 1 || incidents[0].ID != "INC001" {
		t.Errorf("PagerDutyIncidents: got %v", incidents)
	}

	// PagerDutyIncidents with optional params
	_, _ = c.PagerDutyIncidents(context.Background(), "src", "SVC001", "triggered", 5)

	// PagerDutyServices
	services, err := c.PagerDutyServices(context.Background(), "src")
	if err != nil {
		t.Fatalf("PagerDutyServices() error: %v", err)
	}
	if len(services) != 1 || services[0].ID != "SVC001" {
		t.Errorf("PagerDutyServices: got %v", services)
	}

	// GrafanaDashboards
	dashboards, err := c.GrafanaDashboards(context.Background(), "src", "", 50)
	if err != nil {
		t.Fatalf("GrafanaDashboards() error: %v", err)
	}
	if len(dashboards) != 1 || dashboards[0].UID != "dash-1" {
		t.Errorf("GrafanaDashboards: got %v", dashboards)
	}

	// GrafanaDashboards with query
	_, _ = c.GrafanaDashboards(context.Background(), "src", "payment", 10)

	// GrafanaDashboard (single)
	dashboard, err := c.GrafanaDashboard(context.Background(), "src", "dash-1")
	if err != nil {
		t.Fatalf("GrafanaDashboard() error: %v", err)
	}
	if dashboard == nil || dashboard.UID != "dash-1" {
		t.Errorf("GrafanaDashboard: got %v", dashboard)
	}

	// GrafanaAlerts
	galerts, err := c.GrafanaAlerts(context.Background(), "src")
	if err != nil {
		t.Fatalf("GrafanaAlerts() error: %v", err)
	}
	if len(galerts) != 1 || galerts[0].Fingerprint != "fp2" {
		t.Errorf("GrafanaAlerts: got %v", galerts)
	}

	// Verify URL patterns
	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/alertmanager/src/alerts")
	assertContains(t, joined, "/api/v1/pagerduty/src/incidents")
	assertContains(t, joined, "/api/v1/pagerduty/src/services")
	assertContains(t, joined, "/api/v1/grafana/src/dashboards")
	assertContains(t, joined, "/api/v1/grafana/src/dashboards/dash-1")
	assertContains(t, joined, "/api/v1/grafana/src/alerts")
}

func TestAlertingClientErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer ts.Close()

	c := New(ts.URL)

	if _, err := c.AlertmanagerAlerts(context.Background(), "src", ""); err == nil {
		t.Error("AlertmanagerAlerts() expected error, got nil")
	}
	if _, err := c.PagerDutyIncidents(context.Background(), "src", "", "", 10); err == nil {
		t.Error("PagerDutyIncidents() expected error, got nil")
	}
	if _, err := c.PagerDutyServices(context.Background(), "src"); err == nil {
		t.Error("PagerDutyServices() expected error, got nil")
	}
	if _, err := c.GrafanaDashboards(context.Background(), "src", "", 50); err == nil {
		t.Error("GrafanaDashboards() expected error, got nil")
	}
	if _, err := c.GrafanaDashboard(context.Background(), "src", "uid"); err == nil {
		t.Error("GrafanaDashboard() expected error, got nil")
	}
	if _, err := c.GrafanaAlerts(context.Background(), "src"); err == nil {
		t.Error("GrafanaAlerts() expected error, got nil")
	}
}

func TestWithTLS_StillWorks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "v", "time": "t"})
	}))
	defer ts.Close()

	c := New(ts.URL, WithTLS())
	if _, err := c.GetStatus(context.Background()); err != nil {
		t.Fatalf("WithTLS client GetStatus() error: %v", err)
	}
}
