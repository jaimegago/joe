package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/observability"
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
	// Status
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)

	// Graph
	mux.HandleFunc("GET /api/v1/graph/query", s.handleGraphQuery)
	mux.HandleFunc("GET /api/v1/graph/related", s.handleGraphRelated)
	mux.HandleFunc("GET /api/v1/graph/summary", s.handleGraphSummary)

	// Sources
	mux.HandleFunc("GET /api/v1/sources", s.handleListSources)
	mux.HandleFunc("POST /api/v1/sources", s.handleCreateSource)
	mux.HandleFunc("GET /api/v1/sources/{id}", s.handleGetSource)
	mux.HandleFunc("DELETE /api/v1/sources/{id}", s.handleDeleteSource)

	// Kubernetes
	mux.HandleFunc("GET /api/v1/k8s/{sourceID}/resources", s.handleK8sListResources)
	mux.HandleFunc("GET /api/v1/k8s/{sourceID}/resources/{resource}/{namespace}/{name}", s.handleK8sGetResource)
	mux.HandleFunc("GET /api/v1/k8s/{sourceID}/logs/{namespace}/{pod}", s.handleK8sGetLogs)

	// Git
	mux.HandleFunc("GET /api/v1/git/{sourceID}/file", s.handleGitReadFile)
	mux.HandleFunc("GET /api/v1/git/{sourceID}/files", s.handleGitListFiles)
	mux.HandleFunc("GET /api/v1/git/{sourceID}/log", s.handleGitLog)
	mux.HandleFunc("GET /api/v1/git/{sourceID}/diff", s.handleGitDiff)

	// AWS
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/ec2/instances", s.handleAWSEC2ListInstances)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/ec2/instances/{instanceID}", s.handleAWSEC2GetInstance)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/eks/clusters", s.handleAWSEKSListClusters)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/eks/clusters/{clusterName}", s.handleAWSEKSGetCluster)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/rds/instances", s.handleAWSRDSListInstances)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/rds/instances/{dbInstanceID}", s.handleAWSRDSGetInstance)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/vpc/vpcs", s.handleAWSVPCListVPCs)
	mux.HandleFunc("GET /api/v1/aws/{sourceID}/vpc/vpcs/{vpcID}", s.handleAWSVPCGetVPC)

	// Clarifications (placeholder)
	mux.HandleFunc("GET /api/v1/clarifications", s.handleNotImplemented)
	mux.HandleFunc("POST /api/v1/clarifications/{id}/answer", s.handleNotImplemented)
	mux.HandleFunc("POST /api/v1/clarifications/{id}/dismiss", s.handleNotImplemented)

	// Control (placeholder)
	mux.HandleFunc("POST /api/v1/onboarding", s.handleNotImplemented)
	mux.HandleFunc("POST /api/v1/refresh", s.handleNotImplemented)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  statusOK,
		"version": version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleGraphQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'q'")
		return
	}

	nodes, err := s.services.Graph.Query(r.Context(), q)
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

func (s *Server) handleGraphRelated(w http.ResponseWriter, r *http.Request) {
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

	subgraph, err := s.services.Graph.Related(r.Context(), nodeID, depth)
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

func (s *Server) handleGraphSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.services.Graph.Summary(r.Context())
	if err != nil {
		writeInternalError(w, err, "graph summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
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
