package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
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
	s.registerSourceRoutes(mux, apiPrefix, s.services.Store, s.services.Adapters)
	s.registerK8sRoutes(mux, apiPrefix, s.services.Adapters, s.services.Metrics)
	s.registerGitRoutes(mux, apiPrefix, s.services.Adapters, s.services.Metrics)
	s.registerAWSRoutes(mux, apiPrefix, s.services.Adapters, s.services.Metrics)
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
// Requires: store.Store for persistence, adapters.Registry for validation
func (s *Server) registerSourceRoutes(mux *http.ServeMux, prefix string, storeInst *store.Store, registry *adapters.Registry) {
	handler := &sourceHandler{store: storeInst, registry: registry}
	mux.HandleFunc(fmt.Sprintf("GET %s/sources", prefix), handler.handleList)
	mux.HandleFunc(fmt.Sprintf("POST %s/sources", prefix), handler.handleCreate)
	mux.HandleFunc(fmt.Sprintf("GET %s/sources/{id}", prefix), handler.handleGet)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/sources/{id}", prefix), handler.handleDelete)
}

// registerK8sRoutes registers Kubernetes resource routes
// Requires: adapters.Registry to lookup K8s adapters, observability.Metrics for telemetry
func (s *Server) registerK8sRoutes(mux *http.ServeMux, prefix string, registry *adapters.Registry, metrics *observability.Metrics) {
	handler := &k8sHandler{registry: registry, metrics: metrics}
	mux.HandleFunc(fmt.Sprintf("GET %s/k8s/{sourceID}/resources", prefix), handler.handleListResources)
	mux.HandleFunc(fmt.Sprintf("GET %s/k8s/{sourceID}/resources/{resource}/{namespace}/{name}", prefix), handler.handleGetResource)
	mux.HandleFunc(fmt.Sprintf("GET %s/k8s/{sourceID}/logs/{namespace}/{pod}", prefix), handler.handleGetLogs)
}

// registerGitRoutes registers Git repository routes
// Requires: adapters.Registry to lookup Git adapters, observability.Metrics for telemetry
func (s *Server) registerGitRoutes(mux *http.ServeMux, prefix string, registry *adapters.Registry, metrics *observability.Metrics) {
	handler := &gitHandler{registry: registry, metrics: metrics}
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/file", prefix), handler.handleReadFile)
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/files", prefix), handler.handleListFiles)
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/log", prefix), handler.handleLog)
	mux.HandleFunc(fmt.Sprintf("GET %s/git/{sourceID}/diff", prefix), handler.handleDiff)
}

// registerAWSRoutes registers AWS resource routes
// Requires: adapters.Registry to lookup AWS adapters, observability.Metrics for telemetry
func (s *Server) registerAWSRoutes(mux *http.ServeMux, prefix string, registry *adapters.Registry, metrics *observability.Metrics) {
	handler := &awsHandler{registry: registry, metrics: metrics}
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/ec2/instances", prefix), handler.handleEC2ListInstances)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/ec2/instances/{instanceID}", prefix), handler.handleEC2GetInstance)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/eks/clusters", prefix), handler.handleEKSListClusters)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/eks/clusters/{clusterName}", prefix), handler.handleEKSGetCluster)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/rds/instances", prefix), handler.handleRDSListInstances)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/rds/instances/{dbInstanceID}", prefix), handler.handleRDSGetInstance)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/vpc/vpcs", prefix), handler.handleVPCListVPCs)
	mux.HandleFunc(fmt.Sprintf("GET %s/aws/{sourceID}/vpc/vpcs/{vpcID}", prefix), handler.handleVPCGetVPC)
}

// registerClarificationRoutes registers clarification management routes (placeholder)
func (s *Server) registerClarificationRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/clarifications", prefix), s.handleNotImplemented)
	mux.HandleFunc(fmt.Sprintf("POST %s/clarifications/{id}/answer", prefix), s.handleNotImplemented)
	mux.HandleFunc(fmt.Sprintf("POST %s/clarifications/{id}/dismiss", prefix), s.handleNotImplemented)
}

// registerControlRoutes registers control plane routes (placeholder)
func (s *Server) registerControlRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("POST %s/onboarding", prefix), s.handleNotImplemented)
	mux.HandleFunc(fmt.Sprintf("POST %s/refresh", prefix), s.handleNotImplemented)
}

// graphHandler handles graph-related requests
type graphHandler struct {
	graph graph.GraphStore
}

func (h *graphHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'q'")
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
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'nodeID'")
		return
	}

	depth := 1
	if d := r.URL.Query().Get("depth"); d != "" {
		parsed, err := strconv.Atoi(d)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "depth must be a non-negative integer")
			return
		}
		depth = parsed
	}

	subgraph, err := h.graph.Related(r.Context(), nodeID, depth)
	if err != nil {
		if errors.Is(err, graph.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "node not found")
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
	store    *store.Store
	registry *adapters.Registry
}

func (h *sourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in sources.go
	(&Server{services: &core.Services{Store: h.store, Adapters: h.registry}}).handleListSources(w, r)
}

func (h *sourceHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in sources.go
	(&Server{services: &core.Services{Store: h.store, Adapters: h.registry}}).handleCreateSource(w, r)
}

func (h *sourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in sources.go
	(&Server{services: &core.Services{Store: h.store, Adapters: h.registry}}).handleGetSource(w, r)
}

func (h *sourceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in sources.go
	(&Server{services: &core.Services{Store: h.store, Adapters: h.registry}}).handleDeleteSource(w, r)
}

// k8sHandler handles Kubernetes resource requests
type k8sHandler struct {
	registry *adapters.Registry
	metrics  *observability.Metrics
}

func (h *k8sHandler) handleListResources(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in k8s.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleK8sListResources(w, r)
}

func (h *k8sHandler) handleGetResource(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in k8s.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleK8sGetResource(w, r)
}

func (h *k8sHandler) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in k8s.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleK8sGetLogs(w, r)
}

// gitHandler handles Git repository requests
type gitHandler struct {
	registry *adapters.Registry
	metrics  *observability.Metrics
}

func (h *gitHandler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in git.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleGitReadFile(w, r)
}

func (h *gitHandler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in git.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleGitListFiles(w, r)
}

func (h *gitHandler) handleLog(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in git.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleGitLog(w, r)
}

func (h *gitHandler) handleDiff(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in git.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleGitDiff(w, r)
}

// awsHandler handles AWS resource requests
type awsHandler struct {
	registry *adapters.Registry
	metrics  *observability.Metrics
}

func (h *awsHandler) handleEC2ListInstances(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSEC2ListInstances(w, r)
}

func (h *awsHandler) handleEC2GetInstance(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSEC2GetInstance(w, r)
}

func (h *awsHandler) handleEKSListClusters(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSEKSListClusters(w, r)
}

func (h *awsHandler) handleEKSGetCluster(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSEKSGetCluster(w, r)
}

func (h *awsHandler) handleRDSListInstances(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSRDSListInstances(w, r)
}

func (h *awsHandler) handleRDSGetInstance(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSRDSGetInstance(w, r)
}

func (h *awsHandler) handleVPCListVPCs(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSVPCListVPCs(w, r)
}

func (h *awsHandler) handleVPCGetVPC(w http.ResponseWriter, r *http.Request) {
	// Delegate to existing implementation in aws.go
	(&Server{services: &core.Services{Adapters: h.registry, Metrics: h.metrics}}).handleAWSVPCGetVPC(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  statusOK,
		"version": version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleNotImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, errorCodeNotImplemented, errorNotImpl)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}
