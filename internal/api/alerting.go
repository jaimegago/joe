package api

import (
	"net/http"
	"strconv"
	"time"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Alertmanager handlers ---

// handleAlertmanagerAlerts lists active alerts from Alertmanager.
// GET /api/v1/alertmanager/{sourceID}/alerts?filter=<matchers>
func (s *Server) handleAlertmanagerAlerts(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	filter := r.URL.Query().Get("filter")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	alerts, err := s.accessor.AlertmanagerListAlerts(r.Context(), principal, sourceID, filter)
	s.services.Metrics.RecordAdapterCall(r.Context(), "alertmanager", "list_alerts", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Alertmanager") {
			return
		}
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

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	incidents, err := s.accessor.PagerDutyListIncidents(r.Context(), principal, sourceID, serviceID, status, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "pagerduty", "list_incidents", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "PagerDuty") {
			return
		}
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

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	services, err := s.accessor.PagerDutyListServices(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "pagerduty", "list_services", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "PagerDuty") {
			return
		}
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

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	dashboards, err := s.accessor.GrafanaListDashboards(r.Context(), principal, sourceID, query, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "grafana", "list_dashboards", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Grafana") {
			return
		}
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

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	dashboard, err := s.accessor.GrafanaGetDashboard(r.Context(), principal, sourceID, uid)
	s.services.Metrics.RecordAdapterCall(r.Context(), "grafana", "get_dashboard", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Grafana") {
			return
		}
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

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	alerts, err := s.accessor.GrafanaListAlerts(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "grafana", "list_alerts", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Grafana") {
			return
		}
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
