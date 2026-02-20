package coreagent

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// fakeAlertmanagerAdapter satisfies alertmanageradapter.AlertmanagerAdapter.
type fakeAlertmanagerAdapter struct {
	alerts []alertmanageradapter.Alert
	err    error
}

func (f *fakeAlertmanagerAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeAlertmanagerAdapter) Disconnect() error                               { return nil }
func (f *fakeAlertmanagerAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeAlertmanagerAdapter) ListAlerts(_ context.Context, _ string) ([]alertmanageradapter.Alert, error) {
	return f.alerts, f.err
}

// fakePagerDutyAdapter satisfies pagerdutyadapter.PagerDutyAdapter.
type fakePagerDutyAdapter struct {
	incidents []pagerdutyadapter.Incident
	pdSvcs    []pagerdutyadapter.Service
	err       error
}

func (f *fakePagerDutyAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakePagerDutyAdapter) Disconnect() error                               { return nil }
func (f *fakePagerDutyAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakePagerDutyAdapter) ListIncidents(_ context.Context, _, _ string, _ int) ([]pagerdutyadapter.Incident, error) {
	return f.incidents, f.err
}
func (f *fakePagerDutyAdapter) ListServices(_ context.Context) ([]pagerdutyadapter.Service, error) {
	return f.pdSvcs, f.err
}

// fakeGrafanaAdapter satisfies grafanaadapter.GrafanaAdapter.
type fakeGrafanaAdapter struct {
	err error
}

func (f *fakeGrafanaAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (f *fakeGrafanaAdapter) Disconnect() error                               { return nil }
func (f *fakeGrafanaAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (f *fakeGrafanaAdapter) ListDashboards(_ context.Context, _ string, _ int) ([]grafanaadapter.Dashboard, error) {
	return nil, f.err
}
func (f *fakeGrafanaAdapter) GetDashboard(_ context.Context, _ string) (*grafanaadapter.DashboardDetail, error) {
	return nil, f.err
}
func (f *fakeGrafanaAdapter) ListAlerts(_ context.Context) ([]grafanaadapter.GrafanaAlert, error) {
	return nil, f.err
}

// setupAlertingTestServices creates a full services stack (with store) for refreshSource tests.
func setupAlertingTestServices(t *testing.T) (*core.Services, *adapters.Registry) {
	t.Helper()
	sqlStore, err := store.New(":memory:", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	reg := adapters.NewRegistry()
	cfg := &config.Config{}
	svc := core.New(cfg, sqlStore, sqlStore.DB(), reg, nil)
	return svc, reg
}

// ---- alertingNodeID ----

func TestAlertingNodeID(t *testing.T) {
	id := alertingNodeID("src1", "alertmanager")
	want := "alerting/alertmanager/src1"
	if id != want {
		t.Errorf("alertingNodeID = %q, want %q", id, want)
	}
}

// ---- refreshAlertmanagerSource ----

func TestRefreshAlertmanagerSource_NoAlerts(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-am-1", Type: store.SourceTypeAlertmanager, Name: "test-am"}
	adapter := &fakeAlertmanagerAdapter{}

	if err := r.refreshAlertmanagerSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAlertmanagerSource error: %v", err)
	}

	nodes, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "alertmanager_source" {
		t.Errorf("node type = %q, want alertmanager_source", nodes[0].Type)
	}
	if len(edges) != 0 {
		t.Errorf("len(edges) = %d, want 0", len(edges))
	}
}

func TestRefreshAlertmanagerSource_ListAlertsError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-am-2", Type: store.SourceTypeAlertmanager, Name: "test-am"}
	adapter := &fakeAlertmanagerAdapter{err: errors.New("network error")}

	// Should still succeed even if ListAlerts fails (skips edge discovery).
	if err := r.refreshAlertmanagerSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAlertmanagerSource should not error on ListAlerts failure, got: %v", err)
	}
}

func TestRefreshAlertmanagerSource_ActiveAlertWithServiceLabel(t *testing.T) {
	graphStore := setupGraphStore(t)

	// Pre-populate graph with a service node so the edge can be created.
	svcNode := graph.Node{ID: "svc/payment", Type: "service", SourceID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-am-3", Type: store.SourceTypeAlertmanager, Name: "test-am"}
	adapter := &fakeAlertmanagerAdapter{
		alerts: []alertmanageradapter.Alert{
			{
				Fingerprint: "fp1",
				Status:      alertmanageradapter.AlertStatus{State: "active"},
				Labels:      map[string]string{"service": "payment"},
			},
		},
	}

	// Cross-source edges (service → alertmanager) are applied to the graph
	// but won't appear in LoadGraphStateForSource (which only returns edges
	// between nodes of the same source). Just verify no error occurred.
	if err := r.refreshAlertmanagerSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAlertmanagerSource error: %v", err)
	}
}

func TestRefreshAlertmanagerSource_ActiveAlertWithJobLabel(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/api", Type: "deployment", SourceID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-am-4", Type: store.SourceTypeAlertmanager, Name: "test-am"}
	adapter := &fakeAlertmanagerAdapter{
		alerts: []alertmanageradapter.Alert{
			{
				Fingerprint: "fp2",
				Status:      alertmanageradapter.AlertStatus{State: "active"},
				Labels:      map[string]string{"job": "api"}, // no "service", falls back to "job"
			},
		},
	}

	if err := r.refreshAlertmanagerSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAlertmanagerSource error: %v", err)
	}
}

func TestRefreshAlertmanagerSource_AlertNotActive(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-am-5", Type: store.SourceTypeAlertmanager, Name: "test-am"}
	adapter := &fakeAlertmanagerAdapter{
		alerts: []alertmanageradapter.Alert{
			{
				Fingerprint: "fp3",
				Status:      alertmanageradapter.AlertStatus{State: "suppressed"}, // not "active"
				Labels:      map[string]string{"service": "payment"},
			},
		},
	}

	if err := r.refreshAlertmanagerSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAlertmanagerSource error: %v", err)
	}

	_, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("suppressed alert should produce no edges, got %d", len(edges))
	}
}

func TestRefreshAlertmanagerSource_AlertNoServiceOrJob(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-am-6", Type: store.SourceTypeAlertmanager, Name: "test-am"}
	adapter := &fakeAlertmanagerAdapter{
		alerts: []alertmanageradapter.Alert{
			{
				Fingerprint: "fp4",
				Status:      alertmanageradapter.AlertStatus{State: "active"},
				Labels:      map[string]string{"alertname": "WatchdogAlert"}, // no service or job
			},
		},
	}

	if err := r.refreshAlertmanagerSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAlertmanagerSource error: %v", err)
	}
}

// ---- refreshPagerDutySource ----

func TestRefreshPagerDutySource_NoIncidents(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-pd-1", Type: store.SourceTypePagerDuty, Name: "test-pd"}
	adapter := &fakePagerDutyAdapter{}

	if err := r.refreshPagerDutySource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPagerDutySource error: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "pagerduty_source" {
		t.Errorf("node type = %q, want pagerduty_source", nodes[0].Type)
	}
}

func TestRefreshPagerDutySource_ListIncidentsError(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-pd-2", Type: store.SourceTypePagerDuty, Name: "test-pd"}
	adapter := &fakePagerDutyAdapter{err: errors.New("timeout")}

	// Should still succeed (skips edge discovery).
	if err := r.refreshPagerDutySource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPagerDutySource should not error on ListIncidents failure, got: %v", err)
	}
}

func TestRefreshPagerDutySource_WithMatchingService(t *testing.T) {
	graphStore := setupGraphStore(t)

	svcNode := graph.Node{ID: "svc/checkout", Type: "service", SourceID: "src-k8s"}
	if err := graphStore.AddNode(context.Background(), svcNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-pd-3", Type: store.SourceTypePagerDuty, Name: "test-pd"}
	adapter := &fakePagerDutyAdapter{
		incidents: []pagerdutyadapter.Incident{
			{ID: "INC001", Service: pagerdutyadapter.Service{ID: "SVC1", Name: "checkout"}},
		},
	}

	// Cross-source edges are applied but not returned by LoadGraphStateForSource.
	if err := r.refreshPagerDutySource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPagerDutySource error: %v", err)
	}
}

func TestRefreshPagerDutySource_IncidentEmptyServiceName(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-pd-4", Type: store.SourceTypePagerDuty, Name: "test-pd"}
	adapter := &fakePagerDutyAdapter{
		incidents: []pagerdutyadapter.Incident{
			{ID: "INC002", Service: pagerdutyadapter.Service{ID: "SVC2", Name: ""}}, // empty name
		},
	}

	if err := r.refreshPagerDutySource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshPagerDutySource error: %v", err)
	}

	_, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("incident with empty service name should produce no edges, got %d", len(edges))
	}
}

// ---- refreshGrafanaSource ----

func TestRefreshGrafanaSource(t *testing.T) {
	graphStore := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Source{ID: "src-grafana-1", Type: store.SourceTypeGrafana, Name: "test-grafana"}
	adapter := &fakeGrafanaAdapter{}

	if err := r.refreshGrafanaSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshGrafanaSource error: %v", err)
	}

	nodes, _, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "grafana_source" {
		t.Errorf("node type = %q, want grafana_source", nodes[0].Type)
	}
}

// ---- refreshSource switch cases for alerting types ----

func TestRefreshSource_AlertmanagerType(t *testing.T) {
	svc, reg := setupAlertingTestServices(t)
	adapter := &fakeAlertmanagerAdapter{}
	reg.Register("src-am", adapter)

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-am", Type: store.SourceTypeAlertmanager, Name: "am"}
	if err := r.refreshSource(context.Background(), source); err != nil {
		t.Fatalf("refreshSource(alertmanager) error: %v", err)
	}
}

func TestRefreshSource_PagerDutyType(t *testing.T) {
	svc, reg := setupAlertingTestServices(t)
	adapter := &fakePagerDutyAdapter{}
	reg.Register("src-pd", adapter)

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-pd", Type: store.SourceTypePagerDuty, Name: "pd"}
	if err := r.refreshSource(context.Background(), source); err != nil {
		t.Fatalf("refreshSource(pagerduty) error: %v", err)
	}
}

func TestRefreshSource_GrafanaType(t *testing.T) {
	svc, reg := setupAlertingTestServices(t)
	adapter := &fakeGrafanaAdapter{}
	reg.Register("src-grafana", adapter)

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-grafana", Type: store.SourceTypeGrafana, Name: "grafana"}
	if err := r.refreshSource(context.Background(), source); err != nil {
		t.Fatalf("refreshSource(grafana) error: %v", err)
	}
}

func TestRefreshSource_AlertmanagerWrongType(t *testing.T) {
	svc, reg := setupAlertingTestServices(t)
	// Register a pagerduty adapter for an alertmanager source — type assertion will fail.
	reg.Register("src-am-bad", &fakePagerDutyAdapter{})

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-am-bad", Type: store.SourceTypeAlertmanager, Name: "am"}
	if err := r.refreshSource(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshSource_PagerDutyWrongType(t *testing.T) {
	svc, reg := setupAlertingTestServices(t)
	reg.Register("src-pd-bad", &fakeAlertmanagerAdapter{})

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-pd-bad", Type: store.SourceTypePagerDuty, Name: "pd"}
	if err := r.refreshSource(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}

func TestRefreshSource_GrafanaWrongType(t *testing.T) {
	svc, reg := setupAlertingTestServices(t)
	reg.Register("src-grafana-bad", &fakeAlertmanagerAdapter{})

	r := &Refresher{
		services: svc,
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-grafana-bad", Type: store.SourceTypeGrafana, Name: "grafana"}
	if err := r.refreshSource(context.Background(), source); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
