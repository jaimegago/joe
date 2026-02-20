package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
)

func TestAlertingClientEndpoints(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/alertmanager/src/alerts"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"alerts":    []alertmanageradapter.Alert{{Fingerprint: "fp1"}},
				"count":     1,
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/pagerduty/src/incidents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"incidents": []pagerdutyadapter.Incident{{ID: "INC001"}},
				"count":     1,
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/pagerduty/src/services"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"services":  []pagerdutyadapter.Service{{ID: "SVC001", Name: "payment"}},
				"count":     1,
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/grafana/src/dashboards") && !strings.Contains(r.URL.Path, "/dashboards/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dashboards": []grafanaadapter.Dashboard{{UID: "dash-1", Title: "Overview"}},
				"count":      1,
				"source_id":  "src",
			})
		case strings.Contains(r.URL.Path, "/grafana/src/dashboards/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dashboard": &grafanaadapter.DashboardDetail{UID: "dash-1", Title: "Overview"},
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/grafana/src/alerts"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"alerts":    []grafanaadapter.GrafanaAlert{{Fingerprint: "fp2", State: "active"}},
				"count":     1,
				"source_id": "src",
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

func TestObservabilityClientEndpoints(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/prometheus/src/query"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":    &prometheusadapter.QueryResult{ResultType: "vector"},
				"source_id": "src",
				"query":     "up",
			})
		case strings.HasSuffix(r.URL.Path, "/prometheus/src/query_range"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":    &prometheusadapter.QueryResult{ResultType: "matrix"},
				"source_id": "src",
				"query":     "up",
			})
		case strings.HasSuffix(r.URL.Path, "/prometheus/src/targets"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"targets":   []prometheusadapter.Target{{State: "active"}},
				"count":     1,
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/loki/src/query"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":    map[string]any{"result_type": "streams"},
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/loki/src/query_range"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":    map[string]any{"result_type": "streams"},
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/tempo/src/search"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"traces":    []tempoadapter.TraceSearchResult{{TraceID: "abc"}},
				"count":     1,
				"source_id": "src",
			})
		case strings.Contains(r.URL.Path, "/tempo/src/traces/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"trace":     &tempoadapter.Trace{TraceID: "abc123", SpanCount: 3},
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/jaeger/src/services"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"services":  []string{"payment", "order"},
				"count":     2,
				"source_id": "src",
			})
		case strings.HasSuffix(r.URL.Path, "/jaeger/src/traces"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"traces":    []jaegeradapter.TraceSearchResult{{TraceID: "t1"}},
				"count":     1,
				"source_id": "src",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	now := time.Now()
	start := now.Add(-time.Hour)

	// PrometheusQuery (no time)
	result, err := c.PrometheusQuery(context.Background(), "src", "up", time.Time{})
	if err != nil {
		t.Fatalf("PrometheusQuery() error: %v", err)
	}
	if result == nil || result.ResultType != "vector" {
		t.Errorf("PrometheusQuery: unexpected result %v", result)
	}

	// PrometheusQuery with explicit time
	_, _ = c.PrometheusQuery(context.Background(), "src", "up", now)

	// PrometheusQueryRange (with step > 0)
	rangeResult, err := c.PrometheusQueryRange(context.Background(), "src", "up", start, now, 60*time.Second)
	if err != nil {
		t.Fatalf("PrometheusQueryRange() error: %v", err)
	}
	if rangeResult == nil || rangeResult.ResultType != "matrix" {
		t.Errorf("PrometheusQueryRange: unexpected result %v", rangeResult)
	}

	// PrometheusQueryRange with step < 1s (defaults to 15)
	_, _ = c.PrometheusQueryRange(context.Background(), "src", "up", start, now, 0)

	// PrometheusTargets
	targets, err := c.PrometheusTargets(context.Background(), "src")
	if err != nil {
		t.Fatalf("PrometheusTargets() error: %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("PrometheusTargets: got %d targets, want 1", len(targets))
	}

	// LokiQuery
	_, err = c.LokiQuery(context.Background(), "src", `{app="payment"}`, 100, time.Hour)
	if err != nil {
		t.Fatalf("LokiQuery() error: %v", err)
	}

	// LokiQueryRange
	_, err = c.LokiQueryRange(context.Background(), "src", `{app="payment"}`, start, now, 100)
	if err != nil {
		t.Fatalf("LokiQueryRange() error: %v", err)
	}

	// TempoSearch (no optional params)
	traces, err := c.TempoSearch(context.Background(), "src", "", "", 0, 0, 20)
	if err != nil {
		t.Fatalf("TempoSearch() error: %v", err)
	}
	if len(traces) != 1 || traces[0].TraceID != "abc" {
		t.Errorf("TempoSearch: got %v", traces)
	}

	// TempoSearch with optional params
	_, _ = c.TempoSearch(context.Background(), "src", "payment", "http.method=POST", 100, 5000, 10)

	// TempoGetTrace
	trace, err := c.TempoGetTrace(context.Background(), "src", "abc123")
	if err != nil {
		t.Fatalf("TempoGetTrace() error: %v", err)
	}
	if trace == nil || trace.TraceID != "abc123" {
		t.Errorf("TempoGetTrace: got %v", trace)
	}

	// JaegerServices
	svcs, err := c.JaegerServices(context.Background(), "src")
	if err != nil {
		t.Fatalf("JaegerServices() error: %v", err)
	}
	if len(svcs) != 2 {
		t.Errorf("JaegerServices: got %d, want 2", len(svcs))
	}

	// JaegerTraces (no operation)
	jtraces, err := c.JaegerTraces(context.Background(), "src", "payment", "", 20)
	if err != nil {
		t.Fatalf("JaegerTraces() error: %v", err)
	}
	if len(jtraces) != 1 || jtraces[0].TraceID != "t1" {
		t.Errorf("JaegerTraces: got %v", jtraces)
	}

	// JaegerTraces with operation
	_, _ = c.JaegerTraces(context.Background(), "src", "payment", "GET /checkout", 5)

	// Verify key path patterns
	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/prometheus/src/query")
	assertContains(t, joined, "/api/v1/prometheus/src/query_range")
	assertContains(t, joined, "/api/v1/prometheus/src/targets")
	assertContains(t, joined, "/api/v1/loki/src/query")
	assertContains(t, joined, "/api/v1/loki/src/query_range")
	assertContains(t, joined, "/api/v1/tempo/src/search")
	assertContains(t, joined, "/api/v1/tempo/src/traces/abc123")
	assertContains(t, joined, "/api/v1/jaeger/src/services")
	assertContains(t, joined, "/api/v1/jaeger/src/traces")
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

func TestObservabilityClientErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer ts.Close()

	c := New(ts.URL)
	now := time.Now()
	start := now.Add(-time.Hour)

	if _, err := c.PrometheusQuery(context.Background(), "src", "up", time.Time{}); err == nil {
		t.Error("PrometheusQuery() expected error, got nil")
	}
	if _, err := c.PrometheusQueryRange(context.Background(), "src", "up", start, now, 60*time.Second); err == nil {
		t.Error("PrometheusQueryRange() expected error, got nil")
	}
	if _, err := c.PrometheusTargets(context.Background(), "src"); err == nil {
		t.Error("PrometheusTargets() expected error, got nil")
	}
	if _, err := c.LokiQuery(context.Background(), "src", "q", 100, time.Hour); err == nil {
		t.Error("LokiQuery() expected error, got nil")
	}
	if _, err := c.LokiQueryRange(context.Background(), "src", "q", start, now, 100); err == nil {
		t.Error("LokiQueryRange() expected error, got nil")
	}
	if _, err := c.TempoSearch(context.Background(), "src", "", "", 0, 0, 20); err == nil {
		t.Error("TempoSearch() expected error, got nil")
	}
	if _, err := c.TempoGetTrace(context.Background(), "src", "abc"); err == nil {
		t.Error("TempoGetTrace() expected error, got nil")
	}
	if _, err := c.JaegerServices(context.Background(), "src"); err == nil {
		t.Error("JaegerServices() expected error, got nil")
	}
	if _, err := c.JaegerTraces(context.Background(), "src", "svc", "", 10); err == nil {
		t.Error("JaegerTraces() expected error, got nil")
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

func TestAWSVPCWrappers(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/vpc/vpcs") && !strings.Contains(r.URL.Path, "/vpcs/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"vpcs":      []map[string]any{{"vpc_id": "vpc-1"}},
				"count":     1,
				"source_id": "src",
			})
		case strings.Contains(r.URL.Path, "/vpc/vpcs/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"vpc":       map[string]any{"vpc_id": "vpc-1"},
				"source_id": "src",
			})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)

	vpcs, err := c.AWSVPCList(context.Background(), "src")
	if err != nil {
		t.Fatalf("AWSVPCList() error: %v", err)
	}
	if len(vpcs) != 1 {
		t.Errorf("AWSVPCList: got %d VPCs, want 1", len(vpcs))
	}

	vpc, err := c.AWSVPCGet(context.Background(), "src", "vpc-1")
	if err != nil {
		t.Fatalf("AWSVPCGet() error: %v", err)
	}
	if vpc == nil || vpc.VpcID != "vpc-1" {
		t.Errorf("AWSVPCGet: got %v", vpc)
	}
}
