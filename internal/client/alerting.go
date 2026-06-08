package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
)

// --- Alertmanager ---

// AlertmanagerAlerts returns active alerts from Alertmanager.
// filter is an optional label matcher string (e.g., "severity=critical").
func (c *Client) AlertmanagerAlerts(ctx context.Context, sourceID, filter string) ([]alertmanageradapter.Alert, error) {
	u := fmt.Sprintf("%s%s/%s/alerts",
		c.baseURL, apiAlertmanagerBasePath, url.PathEscape(sourceID))
	if filter != "" {
		u += "?filter=" + url.QueryEscape(filter)
	}

	var result struct {
		Alerts      []alertmanageradapter.Alert `json:"alerts"`
		Count       int                         `json:"count"`
		ComponentID string                      `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "alertmanager alerts"); err != nil {
		return nil, err
	}

	return result.Alerts, nil
}

// --- PagerDuty ---

// PagerDutyIncidents returns PagerDuty incidents.
// serviceID and status are optional filters. limit is max results.
func (c *Client) PagerDutyIncidents(ctx context.Context, sourceID, serviceID, status string, limit int) ([]pagerdutyadapter.Incident, error) {
	u := fmt.Sprintf("%s%s/%s/incidents?limit=%d",
		c.baseURL, apiPagerDutyBasePath, url.PathEscape(sourceID), limit)
	if serviceID != "" {
		u += "&service=" + url.QueryEscape(serviceID)
	}
	if status != "" {
		u += "&status=" + url.QueryEscape(status)
	}

	var result struct {
		Incidents   []pagerdutyadapter.Incident `json:"incidents"`
		Count       int                         `json:"count"`
		ComponentID string                      `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "pagerduty incidents"); err != nil {
		return nil, err
	}

	return result.Incidents, nil
}

// PagerDutyServices returns all PagerDuty services.
func (c *Client) PagerDutyServices(ctx context.Context, sourceID string) ([]pagerdutyadapter.Service, error) {
	u := fmt.Sprintf("%s%s/%s/services",
		c.baseURL, apiPagerDutyBasePath, url.PathEscape(sourceID))

	var result struct {
		Services    []pagerdutyadapter.Service `json:"services"`
		Count       int                        `json:"count"`
		ComponentID string                     `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "pagerduty services"); err != nil {
		return nil, err
	}

	return result.Services, nil
}

// --- Grafana ---

// GrafanaDashboards searches for Grafana dashboards.
func (c *Client) GrafanaDashboards(ctx context.Context, sourceID, query string, limit int) ([]grafanaadapter.Dashboard, error) {
	u := fmt.Sprintf("%s%s/%s/dashboards?limit=%s",
		c.baseURL, apiGrafanaBasePath, url.PathEscape(sourceID),
		strconv.Itoa(limit))
	if query != "" {
		u += "&query=" + url.QueryEscape(query)
	}

	var result struct {
		Dashboards  []grafanaadapter.Dashboard `json:"dashboards"`
		Count       int                        `json:"count"`
		ComponentID string                     `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "grafana dashboards"); err != nil {
		return nil, err
	}

	return result.Dashboards, nil
}

// GrafanaDashboard retrieves a single Grafana dashboard by UID.
func (c *Client) GrafanaDashboard(ctx context.Context, sourceID, uid string) (*grafanaadapter.DashboardDetail, error) {
	u := fmt.Sprintf("%s%s/%s/dashboards/%s",
		c.baseURL, apiGrafanaBasePath,
		url.PathEscape(sourceID), url.PathEscape(uid))

	var result struct {
		Dashboard   *grafanaadapter.DashboardDetail `json:"dashboard"`
		ComponentID string                          `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "grafana dashboard"); err != nil {
		return nil, err
	}

	return result.Dashboard, nil
}

// GrafanaAlerts returns active Grafana-managed alerts.
func (c *Client) GrafanaAlerts(ctx context.Context, sourceID string) ([]grafanaadapter.GrafanaAlert, error) {
	u := fmt.Sprintf("%s%s/%s/alerts",
		c.baseURL, apiGrafanaBasePath, url.PathEscape(sourceID))

	var result struct {
		Alerts      []grafanaadapter.GrafanaAlert `json:"alerts"`
		Count       int                           `json:"count"`
		ComponentID string                        `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "grafana alerts"); err != nil {
		return nil, err
	}

	return result.Alerts, nil
}
