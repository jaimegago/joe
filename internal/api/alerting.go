package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
)

// --- Adapter lookup helpers ---

func (s *Server) getAlertmanagerAdapter(sourceID string) (alertmanageradapter.AlertmanagerAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	aa, ok := adapter.(alertmanageradapter.AlertmanagerAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: alertmanager", errInvalidSourceType)
	}
	return aa, nil
}

func (s *Server) getPagerDutyAdapter(sourceID string) (pagerdutyadapter.PagerDutyAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	pa, ok := adapter.(pagerdutyadapter.PagerDutyAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: pagerduty", errInvalidSourceType)
	}
	return pa, nil
}

func (s *Server) getGrafanaAdapter(sourceID string) (grafanaadapter.GrafanaAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	ga, ok := adapter.(grafanaadapter.GrafanaAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: grafana", errInvalidSourceType)
	}
	return ga, nil
}

// --- Alertmanager handlers ---

// handleAlertmanagerAlerts lists active alerts from Alertmanager.
// GET /api/v1/alertmanager/{sourceID}/alerts?filter=<matchers>
func (s *Server) handleAlertmanagerAlerts(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	filter := r.URL.Query().Get("filter")

	aa, err := s.getAlertmanagerAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Alertmanager") {
		return
	}

	start := time.Now()
	alerts, err := aa.ListAlerts(r.Context(), filter)
	s.services.Metrics.RecordAdapterCall(r.Context(), "alertmanager", "list_alerts", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "alertmanager list alerts")
		return
	}

	if alerts == nil {
		alerts = []alertmanageradapter.Alert{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"alerts":    alerts,
		"count":     len(alerts),
		"source_id": sourceID,
	})
}

// --- PagerDuty handlers ---

// handlePagerDutyIncidents lists PagerDuty incidents.
// GET /api/v1/pagerduty/{sourceID}/incidents?service=<id>&status=<status>&limit=<n>
func (s *Server) handlePagerDutyIncidents(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	serviceID := r.URL.Query().Get("service")
	status := r.URL.Query().Get("status")

	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	pa, err := s.getPagerDutyAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "PagerDuty") {
		return
	}

	start := time.Now()
	incidents, err := pa.ListIncidents(r.Context(), serviceID, status, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "pagerduty", "list_incidents", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "pagerduty list incidents")
		return
	}

	if incidents == nil {
		incidents = []pagerdutyadapter.Incident{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"incidents": incidents,
		"count":     len(incidents),
		"source_id": sourceID,
	})
}

// handlePagerDutyServices lists all PagerDuty services.
// GET /api/v1/pagerduty/{sourceID}/services
func (s *Server) handlePagerDutyServices(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	pa, err := s.getPagerDutyAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "PagerDuty") {
		return
	}

	start := time.Now()
	services, err := pa.ListServices(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "pagerduty", "list_services", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "pagerduty list services")
		return
	}

	if services == nil {
		services = []pagerdutyadapter.Service{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"services":  services,
		"count":     len(services),
		"source_id": sourceID,
	})
}

// --- Grafana handlers ---

// handleGrafanaDashboards searches for Grafana dashboards.
// GET /api/v1/grafana/{sourceID}/dashboards?query=<q>&limit=<n>
func (s *Server) handleGrafanaDashboards(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	query := r.URL.Query().Get("query")

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	ga, err := s.getGrafanaAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Grafana") {
		return
	}

	start := time.Now()
	dashboards, err := ga.ListDashboards(r.Context(), query, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "grafana", "list_dashboards", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "grafana list dashboards")
		return
	}

	if dashboards == nil {
		dashboards = []grafanaadapter.Dashboard{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dashboards": dashboards,
		"count":      len(dashboards),
		"source_id":  sourceID,
	})
}

// handleGrafanaGetDashboard retrieves a Grafana dashboard by UID.
// GET /api/v1/grafana/{sourceID}/dashboards/{uid}
func (s *Server) handleGrafanaGetDashboard(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	uid := r.PathValue("uid")

	if uid == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing dashboard UID")
		return
	}

	ga, err := s.getGrafanaAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Grafana") {
		return
	}

	start := time.Now()
	dashboard, err := ga.GetDashboard(r.Context(), uid)
	s.services.Metrics.RecordAdapterCall(r.Context(), "grafana", "get_dashboard", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "grafana get dashboard")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dashboard": dashboard,
		"source_id": sourceID,
	})
}

// handleGrafanaAlerts lists active Grafana-managed alerts.
// GET /api/v1/grafana/{sourceID}/alerts
func (s *Server) handleGrafanaAlerts(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	ga, err := s.getGrafanaAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Grafana") {
		return
	}

	start := time.Now()
	alerts, err := ga.ListAlerts(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "grafana", "list_alerts", time.Since(start), err)
	if err != nil {
		writeInternalError(w, err, "grafana list alerts")
		return
	}

	if alerts == nil {
		alerts = []grafanaadapter.GrafanaAlert{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"alerts":    alerts,
		"count":     len(alerts),
		"source_id": sourceID,
	})
}
