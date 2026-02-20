package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
)

// Server handles HTTP API requests for joecored
type Server struct {
	services *core.Services
}

// New creates a new API server with access to core services
func New(services *core.Services) *Server {
	if services != nil {
		services.Metrics = observability.EnsureMetrics(services.Metrics)
	}
	return &Server{services: services}
}

// RegisterRoutes registers all API routes on the given mux
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.registerStatusRoutes(mux, apiPrefix)
	s.registerGraphRoutes(mux, apiPrefix, s.services.Graph)
	s.registerSourceRoutes(mux, apiPrefix)
	s.registerK8sRoutes(mux, apiPrefix)
	s.registerGitRoutes(mux, apiPrefix)
	s.registerAWSRoutes(mux, apiPrefix)
	s.registerObservabilityRoutes(mux, apiPrefix)
	s.registerAlertingRoutes(mux, apiPrefix)
	s.registerClarificationRoutes(mux, apiPrefix)
	s.registerControlRoutes(mux, apiPrefix)
}

// registerStatusRoutes registers status and health check routes
func (s *Server) registerStatusRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/status", prefix), s.handleStatus)
}

// registerGraphRoutes registers graph query routes
// Requires: graph.GraphStore for querying the knowledge graph
func (s *Server) registerGraphRoutes(mux *http.ServeMux, prefix string, graphStore graph.GraphStore) {
	handler := &graphHandler{graph: graphStore}
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/query", prefix), handler.handleQuery)
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/related", prefix), handler.handleRelated)
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/summary", prefix), handler.handleSummary)
}

// registerSourceRoutes registers source management routes
func (s *Server) registerSourceRoutes(mux *http.ServeMux, prefix string) {
	handler := &sourceHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/sources", prefix), handler.handleList)
	mux.HandleFunc(fmt.Sprintf("POST %s/sources", prefix), handler.handleCreate)
	mux.HandleFunc(fmt.Sprintf("GET %s/sources/{id}", prefix), handler.handleGet)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/sources/{id}", prefix), handler.handleDelete)
}

// registerK8sRoutes registers Kubernetes resource routes
func (s *Server) registerK8sRoutes(mux *http.ServeMux, prefix string) {
	handler := &k8sHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/k8s/{sourceID}/resources", prefix), handler.handleListResources)
	mux.HandleFunc(fmt.Sprintf("GET %s/k8s/{sourceID}/resources/{resource}/{namespace}/{name}", prefix), handler.handleGetResource)
	mux.HandleFunc(fmt.Sprintf("GET %s/k8s/{sourceID}/logs/{namespace}/{pod}", prefix), handler.handleGetLogs)
}

// registerGitRoutes registers Git repository routes
func (s *Server) registerGitRoutes(mux *http.ServeMux, prefix string) {
	handler := &gitHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/file", prefix), handler.handleReadFile)
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/files", prefix), handler.handleListFiles)
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/log", prefix), handler.handleLog)
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/diff", prefix), handler.handleDiff)
}

// registerAWSRoutes registers AWS resource routes
func (s *Server) registerAWSRoutes(mux *http.ServeMux, prefix string) {
	handler := &awsHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/ec2/instances", prefix), handler.handleEC2ListInstances)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/ec2/instances/{instanceID}", prefix), handler.handleEC2GetInstance)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/eks/clusters", prefix), handler.handleEKSListClusters)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/eks/clusters/{clusterName}", prefix), handler.handleEKSGetCluster)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/rds/instances", prefix), handler.handleRDSListInstances)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/rds/instances/{dbInstanceID}", prefix), handler.handleRDSGetInstance)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/vpc/vpcs", prefix), handler.handleVPCListVPCs)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/vpc/vpcs/{vpcID}", prefix), handler.handleVPCGetVPC)
}

// registerObservabilityRoutes registers observability query routes (Prometheus, Loki, Tempo, Jaeger).
func (s *Server) registerObservabilityRoutes(mux *http.ServeMux, prefix string) {
	h := &observabilityHandler{server: s}
	// Prometheus / Mimir
	mux.HandleFunc(fmt.Sprintf("GET %s/prometheus/{sourceID}/query", prefix), h.handlePrometheusQuery)
	mux.HandleFunc(fmt.Sprintf("GET %s/prometheus/{sourceID}/query_range", prefix), h.handlePrometheusQueryRange)
	mux.HandleFunc(fmt.Sprintf("GET %s/prometheus/{sourceID}/targets", prefix), h.handlePrometheusTargets)
	// Loki
	mux.HandleFunc(fmt.Sprintf("GET %s/loki/{sourceID}/query", prefix), h.handleLokiQuery)
	mux.HandleFunc(fmt.Sprintf("GET %s/loki/{sourceID}/query_range", prefix), h.handleLokiQueryRange)
	// Tempo
	mux.HandleFunc(fmt.Sprintf("GET %s/tempo/{sourceID}/search", prefix), h.handleTempoSearch)
	mux.HandleFunc(fmt.Sprintf("GET %s/tempo/{sourceID}/traces/{traceID}", prefix), h.handleTempoGetTrace)
	// Jaeger
	mux.HandleFunc(fmt.Sprintf("GET %s/jaeger/{sourceID}/services", prefix), h.handleJaegerServices)
	mux.HandleFunc(fmt.Sprintf("GET %s/jaeger/{sourceID}/traces", prefix), h.handleJaegerTraces)
	mux.HandleFunc(fmt.Sprintf("GET %s/jaeger/{sourceID}/traces/{traceID}", prefix), h.handleJaegerGetTrace)
}

// observabilityHandler delegates to Server observability methods.
type observabilityHandler struct{ server *Server }

func (h *observabilityHandler) handlePrometheusQuery(w http.ResponseWriter, r *http.Request) {
	h.server.handlePrometheusQuery(w, r)
}
func (h *observabilityHandler) handlePrometheusQueryRange(w http.ResponseWriter, r *http.Request) {
	h.server.handlePrometheusQueryRange(w, r)
}
func (h *observabilityHandler) handlePrometheusTargets(w http.ResponseWriter, r *http.Request) {
	h.server.handlePrometheusTargets(w, r)
}
func (h *observabilityHandler) handleLokiQuery(w http.ResponseWriter, r *http.Request) {
	h.server.handleLokiQuery(w, r)
}
func (h *observabilityHandler) handleLokiQueryRange(w http.ResponseWriter, r *http.Request) {
	h.server.handleLokiQueryRange(w, r)
}
func (h *observabilityHandler) handleTempoSearch(w http.ResponseWriter, r *http.Request) {
	h.server.handleTempoSearch(w, r)
}
func (h *observabilityHandler) handleTempoGetTrace(w http.ResponseWriter, r *http.Request) {
	h.server.handleTempoGetTrace(w, r)
}
func (h *observabilityHandler) handleJaegerServices(w http.ResponseWriter, r *http.Request) {
	h.server.handleJaegerServices(w, r)
}
func (h *observabilityHandler) handleJaegerTraces(w http.ResponseWriter, r *http.Request) {
	h.server.handleJaegerTraces(w, r)
}
func (h *observabilityHandler) handleJaegerGetTrace(w http.ResponseWriter, r *http.Request) {
	h.server.handleJaegerGetTrace(w, r)
}

// registerAlertingRoutes registers alerting query routes (Alertmanager, PagerDuty, Grafana).
func (s *Server) registerAlertingRoutes(mux *http.ServeMux, prefix string) {
	h := &alertingHandler{server: s}
	// Alertmanager
	mux.HandleFunc(fmt.Sprintf("GET %s/alertmanager/{sourceID}/alerts", prefix), h.handleAlertmanagerAlerts)
	// PagerDuty
	mux.HandleFunc(fmt.Sprintf("GET %s/pagerduty/{sourceID}/incidents", prefix), h.handlePagerDutyIncidents)
	mux.HandleFunc(fmt.Sprintf("GET %s/pagerduty/{sourceID}/services", prefix), h.handlePagerDutyServices)
	// Grafana
	mux.HandleFunc(fmt.Sprintf("GET %s/grafana/{sourceID}/dashboards", prefix), h.handleGrafanaDashboards)
	mux.HandleFunc(fmt.Sprintf("GET %s/grafana/{sourceID}/dashboards/{uid}", prefix), h.handleGrafanaGetDashboard)
	mux.HandleFunc(fmt.Sprintf("GET %s/grafana/{sourceID}/alerts", prefix), h.handleGrafanaAlerts)
}

// alertingHandler delegates to Server alerting methods.
type alertingHandler struct{ server *Server }

func (h *alertingHandler) handleAlertmanagerAlerts(w http.ResponseWriter, r *http.Request) {
	h.server.handleAlertmanagerAlerts(w, r)
}
func (h *alertingHandler) handlePagerDutyIncidents(w http.ResponseWriter, r *http.Request) {
	h.server.handlePagerDutyIncidents(w, r)
}
func (h *alertingHandler) handlePagerDutyServices(w http.ResponseWriter, r *http.Request) {
	h.server.handlePagerDutyServices(w, r)
}
func (h *alertingHandler) handleGrafanaDashboards(w http.ResponseWriter, r *http.Request) {
	h.server.handleGrafanaDashboards(w, r)
}
func (h *alertingHandler) handleGrafanaGetDashboard(w http.ResponseWriter, r *http.Request) {
	h.server.handleGrafanaGetDashboard(w, r)
}
func (h *alertingHandler) handleGrafanaAlerts(w http.ResponseWriter, r *http.Request) {
	h.server.handleGrafanaAlerts(w, r)
}

// registerClarificationRoutes registers clarification management routes
func (s *Server) registerClarificationRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.Store == nil {
		return
	}
	handler := &clarificationHandler{
		storeInst:            s.services.Store,
		clarificationService: s.services.Clarifications,
	}
	mux.HandleFunc(fmt.Sprintf("GET %s/clarifications", prefix), handler.handleListClarifications)
	mux.HandleFunc(fmt.Sprintf("POST %s/clarifications/{id}/answer", prefix), handler.handleAnswerClarification)
	mux.HandleFunc(fmt.Sprintf("POST %s/clarifications/{id}/dismiss", prefix), handler.handleDismissClarification)
}

// registerControlRoutes registers control plane routes
func (s *Server) registerControlRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("POST %s/onboarding", prefix), s.handleOnboarding)
	mux.HandleFunc(fmt.Sprintf("POST %s/refresh", prefix), s.handleRefresh)
}

// graphHandler handles graph-related requests
type graphHandler struct {
	graph graph.GraphStore
}

func (h *graphHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'q'", map[string]any{
			"param": "q",
		})
		return
	}

	nodes, err := h.graph.Query(r.Context(), q)
	if err != nil {
		writeInternalError(w, err, "graph query")
		return
	}

	if nodes == nil {
		nodes = []graph.Node{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (h *graphHandler) handleRelated(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeID")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'nodeID'", map[string]any{
			"param": "nodeID",
		})
		return
	}

	depth := 1
	if d := r.URL.Query().Get("depth"); d != "" {
		parsed, err := strconv.Atoi(d)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "depth must be a non-negative integer", map[string]any{
				"param": "depth",
				"value": d,
			})
			return
		}
		depth = parsed
	}

	subgraph, err := h.graph.Related(r.Context(), nodeID, depth)
	if err != nil {
		if errors.Is(err, graph.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "node not found", map[string]any{
				"node_id": nodeID,
			})
			return
		}
		writeInternalError(w, err, "graph related")
		return
	}

	writeJSON(w, http.StatusOK, subgraph)
}

func (h *graphHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.graph.Summary(r.Context())
	if err != nil {
		writeInternalError(w, err, "graph summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// sourceHandler handles source management requests
type sourceHandler struct {
	server *Server
}

func (h *sourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	h.server.handleListSources(w, r)
}

func (h *sourceHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	h.server.handleCreateSource(w, r)
}

func (h *sourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	h.server.handleGetSource(w, r)
}

func (h *sourceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	h.server.handleDeleteSource(w, r)
}

// k8sHandler handles Kubernetes resource requests
type k8sHandler struct {
	server *Server
}

func (h *k8sHandler) handleListResources(w http.ResponseWriter, r *http.Request) {
	h.server.handleK8sListResources(w, r)
}

func (h *k8sHandler) handleGetResource(w http.ResponseWriter, r *http.Request) {
	h.server.handleK8sGetResource(w, r)
}

func (h *k8sHandler) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	h.server.handleK8sGetLogs(w, r)
}

// gitHandler handles Git repository requests
type gitHandler struct {
	server *Server
}

func (h *gitHandler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	h.server.handleGitReadFile(w, r)
}

func (h *gitHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	h.server.handleGitListFiles(w, r)
}

func (h *gitHandler) handleLog(w http.ResponseWriter, r *http.Request) {
	h.server.handleGitLog(w, r)
}

func (h *gitHandler) handleDiff(w http.ResponseWriter, r *http.Request) {
	h.server.handleGitDiff(w, r)
}

// awsHandler handles AWS resource requests
type awsHandler struct {
	server *Server
}

func (h *awsHandler) handleEC2ListInstances(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSEC2ListInstances(w, r)
}

func (h *awsHandler) handleEC2GetInstance(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSEC2GetInstance(w, r)
}

func (h *awsHandler) handleEKSListClusters(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSEKSListClusters(w, r)
}

func (h *awsHandler) handleEKSGetCluster(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSEKSGetCluster(w, r)
}

func (h *awsHandler) handleRDSListInstances(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSRDSListInstances(w, r)
}

func (h *awsHandler) handleRDSGetInstance(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSRDSGetInstance(w, r)
}

func (h *awsHandler) handleVPCListVPCs(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSVPCListVPCs(w, r)
}

func (h *awsHandler) handleVPCGetVPC(w http.ResponseWriter, r *http.Request) {
	h.server.handleAWSVPCGetVPC(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  statusOK,
		"version": version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	if s.services.Agent == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "core agent not available")
		return
	}

	var req struct {
		Input string `json:"input"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid JSON payload", map[string]any{
			"error": err.Error(),
		})
		return
	}

	if req.Input == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required field 'input'")
		return
	}

	if err := s.services.Agent.ProcessOnboarding(r.Context(), req.Input); err != nil {
		writeInternalError(w, err, "onboarding processing")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "onboarding input processed successfully",
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.services.Agent == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "core agent not available")
		return
	}

	var req struct {
		SourceID string `json:"source_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Empty body is OK, treat as full refresh
		if err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid JSON payload", map[string]any{
				"error": err.Error(),
			})
			return
		}
	}

	var err error
	if req.SourceID != "" {
		err = s.services.Agent.TriggerRefreshSource(r.Context(), req.SourceID)
	} else {
		err = s.services.Agent.TriggerRefresh(r.Context())
	}

	if err != nil {
		// Check if source not found
		if req.SourceID != "" && errors.Is(err, store.ErrSourceNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("source '%s' not found", req.SourceID))
			return
		}
		writeInternalError(w, err, "refresh")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "refresh completed successfully",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}
