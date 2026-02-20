package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
)

// --- Adapter lookup helpers ---

func (s *Server) getPrometheusAdapter(sourceID string) (prometheusadapter.PrometheusAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	pa, ok := adapter.(prometheusadapter.PrometheusAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: prometheus", errInvalidSourceType)
	}
	return pa, nil
}

func (s *Server) getLokiAdapter(sourceID string) (lokiadapter.LokiAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	la, ok := adapter.(lokiadapter.LokiAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: loki", errInvalidSourceType)
	}
	return la, nil
}

func (s *Server) getTempoAdapter(sourceID string) (tempoadapter.TempoAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	ta, ok := adapter.(tempoadapter.TempoAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: tempo", errInvalidSourceType)
	}
	return ta, nil
}

func (s *Server) getJaegerAdapter(sourceID string) (jaegeradapter.JaegerAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	ja, ok := adapter.(jaegeradapter.JaegerAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: jaeger", errInvalidSourceType)
	}
	return ja, nil
}

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

	pa, err := s.getPrometheusAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Prometheus") {
		return
	}

	start := time.Now()
	result, err := pa.Query(r.Context(), query, queryTime)
	s.services.Metrics.RecordAdapterCall(r.Context(), "prometheus", "query", time.Since(start), err)
	if err != nil {
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

	pa, err := s.getPrometheusAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Prometheus") {
		return
	}

	start := time.Now()
	result, queryErr := pa.QueryRange(r.Context(), query,
		time.Unix(startUnix, 0), time.Unix(endUnix, 0),
		time.Duration(stepSec)*time.Second)
	s.services.Metrics.RecordAdapterCall(r.Context(), "prometheus", "query_range", time.Since(start), queryErr)
	if queryErr != nil {
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

	pa, err := s.getPrometheusAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Prometheus") {
		return
	}

	start := time.Now()
	targets, err := pa.Targets(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "prometheus", "targets", time.Since(start), err)
	if err != nil {
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

	la, err := s.getLokiAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Loki") {
		return
	}

	start := time.Now()
	result, err := la.Query(r.Context(), query, limit, since)
	s.services.Metrics.RecordAdapterCall(r.Context(), "loki", "query", time.Since(start), err)
	if err != nil {
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

	la, err := s.getLokiAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Loki") {
		return
	}

	start := time.Now()
	result, queryErr := la.QueryRange(r.Context(), query,
		time.Unix(startUnix, 0), time.Unix(endUnix, 0), limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "loki", "query_range", time.Since(start), queryErr)
	if queryErr != nil {
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

	ta, err := s.getTempoAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Tempo") {
		return
	}

	start := time.Now()
	results, err := ta.Search(r.Context(), service, tags, minDuration, maxDuration, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "tempo", "search", time.Since(start), err)
	if err != nil {
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

	ta, err := s.getTempoAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Tempo") {
		return
	}

	start := time.Now()
	trace, err := ta.GetTrace(r.Context(), traceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "tempo", "get_trace", time.Since(start), err)
	if err != nil {
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

	ja, err := s.getJaegerAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Jaeger") {
		return
	}

	start := time.Now()
	services, err := ja.ListServices(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "jaeger", "list_services", time.Since(start), err)
	if err != nil {
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

	ja, err := s.getJaegerAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Jaeger") {
		return
	}

	start := time.Now()
	traces, err := ja.SearchTraces(r.Context(), service, operation, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "jaeger", "search_traces", time.Since(start), err)
	if err != nil {
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

	ja, err := s.getJaegerAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "Jaeger") {
		return
	}

	start := time.Now()
	trace, err := ja.GetTrace(r.Context(), traceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "jaeger", "get_trace", time.Since(start), err)
	if err != nil {
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
