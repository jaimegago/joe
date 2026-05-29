package access

import (
	"context"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Alertmanager ---

func (a *Accessor) AlertmanagerListAlerts(ctx context.Context, principal rbac.Principal, sourceID, filter string) ([]alertmanageradapter.Alert, error) {
	ad, err := guard[alertmanageradapter.AlertmanagerAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "alertmanager")
	if err != nil {
		return nil, err
	}
	return ad.ListAlerts(ctx, filter)
}

// --- PagerDuty ---

func (a *Accessor) PagerDutyListIncidents(ctx context.Context, principal rbac.Principal, sourceID, serviceID, status string, limit int) ([]pagerdutyadapter.Incident, error) {
	ad, err := guard[pagerdutyadapter.PagerDutyAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "pagerduty")
	if err != nil {
		return nil, err
	}
	return ad.ListIncidents(ctx, serviceID, status, limit)
}

func (a *Accessor) PagerDutyListServices(ctx context.Context, principal rbac.Principal, sourceID string) ([]pagerdutyadapter.Service, error) {
	ad, err := guard[pagerdutyadapter.PagerDutyAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "pagerduty")
	if err != nil {
		return nil, err
	}
	return ad.ListServices(ctx)
}

// --- Grafana ---

func (a *Accessor) GrafanaListDashboards(ctx context.Context, principal rbac.Principal, sourceID, query string, limit int) ([]grafanaadapter.Dashboard, error) {
	ad, err := guard[grafanaadapter.GrafanaAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "grafana")
	if err != nil {
		return nil, err
	}
	return ad.ListDashboards(ctx, query, limit)
}

func (a *Accessor) GrafanaGetDashboard(ctx context.Context, principal rbac.Principal, sourceID, uid string) (*grafanaadapter.DashboardDetail, error) {
	ad, err := guard[grafanaadapter.GrafanaAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "grafana")
	if err != nil {
		return nil, err
	}
	return ad.GetDashboard(ctx, uid)
}

func (a *Accessor) GrafanaListAlerts(ctx context.Context, principal rbac.Principal, sourceID string) ([]grafanaadapter.GrafanaAlert, error) {
	ad, err := guard[grafanaadapter.GrafanaAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "grafana")
	if err != nil {
		return nil, err
	}
	return ad.ListAlerts(ctx)
}
