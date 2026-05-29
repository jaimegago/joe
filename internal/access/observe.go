package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/observe"
	"github.com/jaimegago/joe/internal/rbac"
)

// The category-based observe API resolves a source whose concrete adapter
// type is not known at the call site (it is discovered by type-switching the
// resolved adapter). These methods keep that dispatch — and every adapter
// method call — inside the accessor, so no ungoverned path is created. They
// enforce ActionRead; supported is false when the resolved adapter has no
// path for the requested category (the handler turns that into a 400).

// observeResolve enforces the declared action and resolves the base adapter.
// It is a thin sibling of guard for the type-switch dispatchers below; the
// action is passed in by each dispatcher so the declaration stays adjacent to
// the method (design §2.8).
func (a *Accessor) observeResolve(ctx context.Context, principal rbac.Principal, sourceID string, action rbac.Action) (adapters.Adapter, error) {
	if err := a.permitForPrincipal(ctx, principal, sourceID, action); err != nil {
		return nil, err
	}
	adapter, err := a.registry.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrSourceNotFound, sourceID)
		}
		return nil, err
	}
	return adapter, nil
}

// ObserveMetrics runs the metrics query appropriate to the source's adapter.
func (a *Accessor) ObserveMetrics(ctx context.Context, principal rbac.Principal, sourceID, nativeQuery string, fromUnix, toUnix int64) (raw any, supported bool, err error) {
	adapter, err := a.observeResolve(ctx, principal, sourceID, rbac.ActionRead)
	if err != nil {
		return nil, false, err
	}
	switch ad := adapter.(type) {
	case prometheusadapter.PrometheusAdapter:
		r, qErr := ad.Query(ctx, nativeQuery, time.Time{})
		return r, true, qErr
	case datadogadapter.DatadogAdapter:
		r, qErr := ad.MetricsQuery(ctx, nativeQuery, fromUnix, toUnix)
		return r, true, qErr
	case dynatraceadapter.DynatraceAdapter:
		r, qErr := ad.MetricsQuery(ctx, nativeQuery, fromUnix*1000, toUnix*1000) // Dynatrace uses ms
		return r, true, qErr
	case newrelicadapter.NewRelicAdapter:
		r, qErr := ad.NRQLQuery(ctx, 0, nativeQuery)
		return r, true, qErr
	default:
		return nil, false, nil
	}
}

// ObserveLogs runs the logs query appropriate to the source's adapter.
func (a *Accessor) ObserveLogs(ctx context.Context, principal rbac.Principal, sourceID, nativeQuery string, fromUnix, toUnix int64) (raw any, supported bool, err error) {
	adapter, err := a.observeResolve(ctx, principal, sourceID, rbac.ActionRead)
	if err != nil {
		return nil, false, err
	}
	switch ad := adapter.(type) {
	case lokiadapter.LokiAdapter:
		r, qErr := ad.Query(ctx, nativeQuery, 100, time.Hour)
		return r, true, qErr
	case datadogadapter.DatadogAdapter:
		r, qErr := ad.LogsSearch(ctx, nativeQuery, fromUnix, toUnix, 100)
		return r, true, qErr
	case splunkadapter.SplunkAdapter:
		r, qErr := ad.Search(ctx, nativeQuery, "-1h", "now", 100)
		return r, true, qErr
	default:
		return nil, false, nil
	}
}

// ObserveTraces runs the traces search appropriate to the source's adapter.
func (a *Accessor) ObserveTraces(ctx context.Context, principal rbac.Principal, sourceID, service string) (raw any, supported bool, err error) {
	adapter, err := a.observeResolve(ctx, principal, sourceID, rbac.ActionRead)
	if err != nil {
		return nil, false, err
	}
	switch ad := adapter.(type) {
	case tempoadapter.TempoAdapter:
		r, qErr := ad.Search(ctx, service, "", 0, 0, 20)
		return r, true, qErr
	case jaegeradapter.JaegerAdapter:
		r, qErr := ad.SearchTraces(ctx, service, "", 20)
		return r, true, qErr
	default:
		return nil, false, nil
	}
}

// ObserveAlerts lists alerts appropriate to the source's adapter, normalized
// to []observe.Alert. supported is false for adapters with no alerts path.
func (a *Accessor) ObserveAlerts(ctx context.Context, principal rbac.Principal, sourceID, filter string) (alerts []observe.Alert, supported bool, err error) {
	adapter, err := a.observeResolve(ctx, principal, sourceID, rbac.ActionRead)
	if err != nil {
		return nil, false, err
	}
	switch ad := adapter.(type) {
	case alertmanageradapter.AlertmanagerAdapter:
		raw, qErr := ad.ListAlerts(ctx, filter)
		if qErr != nil {
			return nil, true, qErr
		}
		out := make([]observe.Alert, 0, len(raw))
		for _, alert := range raw {
			out = append(out, observe.Alert{
				Name:    alert.Labels["alertname"],
				State:   alert.Status.State,
				Labels:  alert.Labels,
				Summary: alert.Annotations["summary"],
			})
		}
		return out, true, nil
	case pagerdutyadapter.PagerDutyAdapter:
		incidents, qErr := ad.ListIncidents(ctx, "", "triggered,acknowledged", 50)
		if qErr != nil {
			return nil, true, qErr
		}
		out := make([]observe.Alert, 0, len(incidents))
		for _, inc := range incidents {
			out = append(out, observe.Alert{
				Name:    inc.Title,
				State:   inc.Status,
				Summary: inc.Description,
			})
		}
		return out, true, nil
	default:
		return nil, false, nil
	}
}
