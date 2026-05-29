package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Prometheus handlers ---

// handlePrometheusQuery executes an instant PromQL query.
// GET /api/v1/prometheus/{sourceID}/query?query=<promql>&time=<unix>
func (s *Server) handlePrometheusQuery(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query", map[string]any{
			"param": "query",
		})
		return
	}

	var queryTime time.Time
	if ts := r.URL.Query().Get("time"); ts != "" {
		unix, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid time parameter (expected unix timestamp)")
			return
		}
		queryTime = time.Unix(unix, 0)
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.PrometheusQuery(r.Context(), principal, sourceID, query, queryTime)
	s.services.Metrics.RecordAdapterCall(r.Context(), "prometheus", "query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Prometheus") {
			return
		}
		writeInternalError(w, err, "prometheus query")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// handlePrometheusQueryRange executes a range PromQL query.
// GET /api/v1/prometheus/{sourceID}/query_range?query=<promql>&start=<unix>&end=<unix>&step=<sec>
func (s *Server) handlePrometheusQueryRange(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query", map[string]any{
			"param": "query",
		})
		return
	}

	startUnix, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid start parameter (expected unix timestamp)")
		return
	}
	endUnix, err := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid end parameter (expected unix timestamp)")
		return
	}

	stepSec := int64(15)
	if s := r.URL.Query().Get("step"); s != "" {
		stepSec, err = strconv.ParseInt(s, 10, 64)
		if err != nil || stepSec < 1 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid step parameter (expected positive integer seconds)")
			return
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, queryErr := s.accessor.PrometheusQueryRange(r.Context(), principal, sourceID, query,
		time.Unix(startUnix, 0), time.Unix(endUnix, 0),
		time.Duration(stepSec)*time.Second)
	s.services.Metrics.RecordAdapterCall(r.Context(), "prometheus", "query_range", time.Since(start), queryErr)
	if queryErr != nil {
		if handleAccessError(w, queryErr, sourceID, "Prometheus") {
			return
		}
		writeInternalError(w, queryErr, "prometheus query_range")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// handlePrometheusTargets lists Prometheus scrape targets.
// GET /api/v1/prometheus/{sourceID}/targets
func (s *Server) handlePrometheusTargets(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	targets, err := s.accessor.PrometheusTargets(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "prometheus", "targets", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Prometheus") {
			return
		}
		writeInternalError(w, err, "prometheus targets")
		return
	}

	if targets == nil {
		targets = []prometheusadapter.Target{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"targets":   targets,
		"count":     len(targets),
		"source_id": sourceID,
	})
}

// --- Loki handlers ---

// handleLokiQuery executes an instant LogQL query.
// GET /api/v1/loki/{sourceID}/query?query=<logql>&limit=<n>&since=<sec>
func (s *Server) handleLokiQuery(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query", map[string]any{
			"param": "query",
		})
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	since := time.Hour
	if s := r.URL.Query().Get("since"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
			since = time.Duration(v) * time.Second
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.LokiQuery(r.Context(), principal, sourceID, query, limit, since)
	s.services.Metrics.RecordAdapterCall(r.Context(), "loki", "query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Loki") {
			return
		}
		writeInternalError(w, err, "loki query")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// handleLokiQueryRange executes a range LogQL query.
// GET /api/v1/loki/{sourceID}/query_range?query=<logql>&start=<unix>&end=<unix>&limit=<n>
func (s *Server) handleLokiQueryRange(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query")
		return
	}

	startUnix, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid start parameter (expected unix timestamp)")
		return
	}
	endUnix, err := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid end parameter (expected unix timestamp)")
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, queryErr := s.accessor.LokiQueryRange(r.Context(), principal, sourceID, query,
		time.Unix(startUnix, 0), time.Unix(endUnix, 0), limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "loki", "query_range", time.Since(start), queryErr)
	if queryErr != nil {
		if handleAccessError(w, queryErr, sourceID, "Loki") {
			return
		}
		writeInternalError(w, queryErr, "loki query_range")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// --- Tempo handlers ---

// handleTempoSearch searches for traces.
// GET /api/v1/tempo/{sourceID}/search?service=<name>&tags=<tags>&min_duration=<ms>&max_duration=<ms>&limit=<n>
func (s *Server) handleTempoSearch(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	service := r.URL.Query().Get("service")
	tags := r.URL.Query().Get("tags")

	minDuration := 0
	if m := r.URL.Query().Get("min_duration"); m != "" {
		if v, err := strconv.Atoi(m); err == nil {
			minDuration = v
		}
	}
	maxDuration := 0
	if m := r.URL.Query().Get("max_duration"); m != "" {
		if v, err := strconv.Atoi(m); err == nil {
			maxDuration = v
		}
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	results, err := s.accessor.TempoSearch(r.Context(), principal, sourceID, service, tags, minDuration, maxDuration, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "tempo", "search", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Tempo") {
			return
		}
		writeInternalError(w, err, "tempo search")
		return
	}

	if results == nil {
		results = []tempoadapter.TraceSearchResult{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"traces":    results,
		"count":     len(results),
		"source_id": sourceID,
	})
}

// handleTempoGetTrace retrieves a full trace by ID.
// GET /api/v1/tempo/{sourceID}/traces/{traceID}
func (s *Server) handleTempoGetTrace(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	traceID := r.PathValue("traceID")

	if traceID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing trace ID")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	trace, err := s.accessor.TempoGetTrace(r.Context(), principal, sourceID, traceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "tempo", "get_trace", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Tempo") {
			return
		}
		if errors.Is(err, tempoadapter.ErrTraceNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("trace not found: %s", traceID), map[string]any{
				"trace_id": traceID,
			})
			return
		}
		writeInternalError(w, err, "tempo get trace")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"trace":     trace,
		"source_id": sourceID,
	})
}

// --- Jaeger handlers ---

// handleJaegerServices lists all services Jaeger knows about.
// GET /api/v1/jaeger/{sourceID}/services
func (s *Server) handleJaegerServices(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	services, err := s.accessor.JaegerServices(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "jaeger", "list_services", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Jaeger") {
			return
		}
		writeInternalError(w, err, "jaeger list services")
		return
	}

	if services == nil {
		services = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"services":  services,
		"count":     len(services),
		"source_id": sourceID,
	})
}

// handleJaegerTraces searches for traces by service.
// GET /api/v1/jaeger/{sourceID}/traces?service=<name>&operation=<op>&limit=<n>
func (s *Server) handleJaegerTraces(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	service := r.URL.Query().Get("service")
	if service == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: service", map[string]any{
			"param": "service",
		})
		return
	}

	operation := r.URL.Query().Get("operation")

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	traces, err := s.accessor.JaegerSearchTraces(r.Context(), principal, sourceID, service, operation, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "jaeger", "search_traces", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Jaeger") {
			return
		}
		writeInternalError(w, err, "jaeger search traces")
		return
	}

	if traces == nil {
		traces = []jaegeradapter.TraceSearchResult{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"traces":    traces,
		"count":     len(traces),
		"source_id": sourceID,
	})
}

// handleJaegerGetTrace retrieves a full trace by ID.
// GET /api/v1/jaeger/{sourceID}/traces/{traceID}
func (s *Server) handleJaegerGetTrace(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	traceID := r.PathValue("traceID")

	if traceID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing trace ID")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	trace, err := s.accessor.JaegerGetTrace(r.Context(), principal, sourceID, traceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "jaeger", "get_trace", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Jaeger") {
			return
		}
		if errors.Is(err, jaegeradapter.ErrTraceNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("trace not found: %s", traceID), map[string]any{
				"trace_id": traceID,
			})
			return
		}
		writeInternalError(w, err, "jaeger get trace")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"trace":     trace,
		"source_id": sourceID,
	})
}

// --- Datadog handlers ---

// handleDatadogMetrics executes a Datadog metrics query.
// GET /api/v1/datadog/{sourceID}/metrics?query=<q>&from=<unix>&to=<unix>
func (s *Server) handleDatadogMetrics(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query")
		return
	}

	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	if f := r.URL.Query().Get("from"); f != "" {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil {
			from = v
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			to = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.DatadogMetricsQuery(r.Context(), principal, sourceID, query, from, to)
	s.services.Metrics.RecordAdapterCall(r.Context(), "datadog", "metrics_query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Datadog") {
			return
		}
		writeInternalError(w, err, "datadog metrics query")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// handleDatadogLogs searches Datadog log events.
// GET /api/v1/datadog/{sourceID}/logs?query=<q>&from=<unix>&to=<unix>&limit=<n>
func (s *Server) handleDatadogLogs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query")
		return
	}

	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	if f := r.URL.Query().Get("from"); f != "" {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil {
			from = v
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			to = v
		}
	}

	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.DatadogLogsSearch(r.Context(), principal, sourceID, query, from, to, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "datadog", "logs_search", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Datadog") {
			return
		}
		writeInternalError(w, err, "datadog logs search")
		return
	}

	if result == nil {
		result = &datadogadapter.LogsResult{Logs: []datadogadapter.LogEntry{}}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// --- Splunk handlers ---

// handleSplunkSearch executes a Splunk SPL search.
// GET /api/v1/splunk/{sourceID}/search?query=<spl>&earliest=<t>&latest=<t>&limit=<n>
func (s *Server) handleSplunkSearch(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query")
		return
	}

	earliest := r.URL.Query().Get("earliest")
	if earliest == "" {
		earliest = "-1h"
	}
	latest := r.URL.Query().Get("latest")
	if latest == "" {
		latest = "now"
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.SplunkSearch(r.Context(), principal, sourceID, query, earliest, latest, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "splunk", "search", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Splunk") {
			return
		}
		writeInternalError(w, err, "splunk search")
		return
	}

	if result == nil {
		result = &splunkadapter.SearchResult{Events: []splunkadapter.SearchEvent{}}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// --- Dynatrace handlers ---

// handleDynatraceMetrics executes a Dynatrace metrics selector query.
// GET /api/v1/dynatrace/{sourceID}/metrics?query=<selector>&from=<ms>&to=<ms>
func (s *Server) handleDynatraceMetrics(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query")
		return
	}

	now := time.Now()
	from := now.Add(-time.Hour).UnixMilli()
	to := now.UnixMilli()

	if f := r.URL.Query().Get("from"); f != "" {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil {
			from = v
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			to = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.DynatraceMetricsQuery(r.Context(), principal, sourceID, query, from, to)
	s.services.Metrics.RecordAdapterCall(r.Context(), "dynatrace", "metrics_query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Dynatrace") {
			return
		}
		writeInternalError(w, err, "dynatrace metrics query")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}

// handleDynatraceEvents returns Dynatrace events.
// GET /api/v1/dynatrace/{sourceID}/events?from=<ms>&to=<ms>&limit=<n>
func (s *Server) handleDynatraceEvents(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	now := time.Now()
	from := now.Add(-time.Hour).UnixMilli()
	to := now.UnixMilli()

	if f := r.URL.Query().Get("from"); f != "" {
		if v, err := strconv.ParseInt(f, 10, 64); err == nil {
			from = v
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if v, err := strconv.ParseInt(t, 10, 64); err == nil {
			to = v
		}
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.DynatraceEvents(r.Context(), principal, sourceID, from, to, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "dynatrace", "events", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Dynatrace") {
			return
		}
		writeInternalError(w, err, "dynatrace events")
		return
	}

	if result == nil {
		result = &dynatraceadapter.EventsResult{Events: []dynatraceadapter.DynatraceEvent{}}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
	})
}

// --- New Relic handlers ---

// handleNewRelicNRQL executes a New Relic NRQL query.
// GET /api/v1/newrelic/{sourceID}/nrql?query=<nrql>&account_id=<id>
func (s *Server) handleNewRelicNRQL(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	query := r.URL.Query().Get("query")
	if query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: query")
		return
	}

	accountID := 0
	if id := r.URL.Query().Get("account_id"); id != "" {
		if v, err := strconv.Atoi(id); err == nil {
			accountID = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	result, err := s.accessor.NewRelicNRQL(r.Context(), principal, sourceID, accountID, query)
	s.services.Metrics.RecordAdapterCall(r.Context(), "newrelic", "nrql_query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "New Relic") {
			return
		}
		writeInternalError(w, err, "newrelic nrql query")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"result":    result,
		"source_id": sourceID,
		"query":     query,
	})
}
