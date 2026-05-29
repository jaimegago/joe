package access

import (
	"context"
	"time"

	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Prometheus ---

func (a *Accessor) PrometheusQuery(ctx context.Context, principal rbac.Principal, sourceID, query string, queryTime time.Time) (*prometheusadapter.QueryResult, error) {
	ad, err := guard[prometheusadapter.PrometheusAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "prometheus")
	if err != nil {
		return nil, err
	}
	return ad.Query(ctx, query, queryTime)
}

func (a *Accessor) PrometheusQueryRange(ctx context.Context, principal rbac.Principal, sourceID, query string, start, end time.Time, step time.Duration) (*prometheusadapter.QueryResult, error) {
	ad, err := guard[prometheusadapter.PrometheusAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "prometheus")
	if err != nil {
		return nil, err
	}
	return ad.QueryRange(ctx, query, start, end, step)
}

func (a *Accessor) PrometheusTargets(ctx context.Context, principal rbac.Principal, sourceID string) ([]prometheusadapter.Target, error) {
	ad, err := guard[prometheusadapter.PrometheusAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "prometheus")
	if err != nil {
		return nil, err
	}
	return ad.Targets(ctx)
}

// --- Loki ---

func (a *Accessor) LokiQuery(ctx context.Context, principal rbac.Principal, sourceID, query string, limit int, since time.Duration) (*lokiadapter.QueryResult, error) {
	ad, err := guard[lokiadapter.LokiAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "loki")
	if err != nil {
		return nil, err
	}
	return ad.Query(ctx, query, limit, since)
}

func (a *Accessor) LokiQueryRange(ctx context.Context, principal rbac.Principal, sourceID, query string, start, end time.Time, limit int) (*lokiadapter.QueryResult, error) {
	ad, err := guard[lokiadapter.LokiAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "loki")
	if err != nil {
		return nil, err
	}
	return ad.QueryRange(ctx, query, start, end, limit)
}

// --- Tempo ---

func (a *Accessor) TempoSearch(ctx context.Context, principal rbac.Principal, sourceID, service, tags string, minDurationMs, maxDurationMs, limit int) ([]tempoadapter.TraceSearchResult, error) {
	ad, err := guard[tempoadapter.TempoAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "tempo")
	if err != nil {
		return nil, err
	}
	return ad.Search(ctx, service, tags, minDurationMs, maxDurationMs, limit)
}

func (a *Accessor) TempoGetTrace(ctx context.Context, principal rbac.Principal, sourceID, traceID string) (*tempoadapter.Trace, error) {
	ad, err := guard[tempoadapter.TempoAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "tempo")
	if err != nil {
		return nil, err
	}
	return ad.GetTrace(ctx, traceID)
}

// --- Jaeger ---

func (a *Accessor) JaegerServices(ctx context.Context, principal rbac.Principal, sourceID string) ([]string, error) {
	ad, err := guard[jaegeradapter.JaegerAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "jaeger")
	if err != nil {
		return nil, err
	}
	return ad.ListServices(ctx)
}

func (a *Accessor) JaegerSearchTraces(ctx context.Context, principal rbac.Principal, sourceID, service, operation string, limit int) ([]jaegeradapter.TraceSearchResult, error) {
	ad, err := guard[jaegeradapter.JaegerAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "jaeger")
	if err != nil {
		return nil, err
	}
	return ad.SearchTraces(ctx, service, operation, limit)
}

func (a *Accessor) JaegerGetTrace(ctx context.Context, principal rbac.Principal, sourceID, traceID string) (map[string]any, error) {
	ad, err := guard[jaegeradapter.JaegerAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "jaeger")
	if err != nil {
		return nil, err
	}
	return ad.GetTrace(ctx, traceID)
}

// --- Datadog ---

func (a *Accessor) DatadogMetricsQuery(ctx context.Context, principal rbac.Principal, sourceID, query string, from, to int64) (*datadogadapter.MetricsResult, error) {
	ad, err := guard[datadogadapter.DatadogAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "datadog")
	if err != nil {
		return nil, err
	}
	return ad.MetricsQuery(ctx, query, from, to)
}

func (a *Accessor) DatadogLogsSearch(ctx context.Context, principal rbac.Principal, sourceID, query string, from, to int64, limit int) (*datadogadapter.LogsResult, error) {
	ad, err := guard[datadogadapter.DatadogAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "datadog")
	if err != nil {
		return nil, err
	}
	return ad.LogsSearch(ctx, query, from, to, limit)
}

// --- Splunk ---

func (a *Accessor) SplunkSearch(ctx context.Context, principal rbac.Principal, sourceID, query, earliest, latest string, limit int) (*splunkadapter.SearchResult, error) {
	ad, err := guard[splunkadapter.SplunkAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "splunk")
	if err != nil {
		return nil, err
	}
	return ad.Search(ctx, query, earliest, latest, limit)
}

// --- Dynatrace ---

func (a *Accessor) DynatraceMetricsQuery(ctx context.Context, principal rbac.Principal, sourceID, query string, from, to int64) (*dynatraceadapter.MetricsResult, error) {
	ad, err := guard[dynatraceadapter.DynatraceAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "dynatrace")
	if err != nil {
		return nil, err
	}
	return ad.MetricsQuery(ctx, query, from, to)
}

func (a *Accessor) DynatraceEvents(ctx context.Context, principal rbac.Principal, sourceID string, from, to int64, limit int) (*dynatraceadapter.EventsResult, error) {
	ad, err := guard[dynatraceadapter.DynatraceAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "dynatrace")
	if err != nil {
		return nil, err
	}
	return ad.Events(ctx, from, to, limit)
}

// --- New Relic ---

func (a *Accessor) NewRelicNRQL(ctx context.Context, principal rbac.Principal, sourceID string, accountID int, query string) (*newrelicadapter.NRQLResult, error) {
	ad, err := guard[newrelicadapter.NewRelicAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "newrelic")
	if err != nil {
		return nil, err
	}
	return ad.NRQLQuery(ctx, accountID, query)
}
