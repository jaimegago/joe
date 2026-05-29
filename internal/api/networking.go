package api

import (
	"fmt"
	"net/http"
	"time"

	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/rbac"
)

// =========================
// NGINX handlers
// =========================

// handleNginxIngresses lists Ingress resources.
// GET /api/v1/nginx/{sourceID}/ingresses?namespace=
func (s *Server) handleNginxIngresses(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	namespace := r.URL.Query().Get("namespace")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	ingresses, err := s.accessor.NginxListIngresses(r.Context(), principal, sourceID, namespace)
	s.services.Metrics.RecordAdapterCall(r.Context(), "nginx", "ingresses", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "nginx-ingress") {
			return
		}
		writeInternalError(w, err, "nginx ingresses")
		return
	}

	if ingresses == nil {
		ingresses = []nginxadapter.Ingress{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ingresses": ingresses,
		"count":     len(ingresses),
		"source_id": sourceID,
		"namespace": namespace,
	})
}

// handleNginxStatus returns NGINX connection statistics.
// GET /api/v1/nginx/{sourceID}/status
func (s *Server) handleNginxStatus(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	status, err := s.accessor.NginxGetStatus(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "nginx", "status", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "nginx-ingress") {
			return
		}
		writeInternalError(w, err, "nginx status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    status,
		"source_id": sourceID,
	})
}

// handleNginxConfigMaps lists NGINX controller ConfigMaps.
// GET /api/v1/nginx/{sourceID}/config?namespace=
func (s *Server) handleNginxConfigMaps(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	namespace := r.URL.Query().Get("namespace")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	cms, err := s.accessor.NginxListConfigMaps(r.Context(), principal, sourceID, namespace)
	s.services.Metrics.RecordAdapterCall(r.Context(), "nginx", "configmaps", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "nginx-ingress") {
			return
		}
		writeInternalError(w, err, "nginx configmaps")
		return
	}

	if cms == nil {
		cms = []nginxadapter.ConfigMapSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config_maps": cms,
		"count":       len(cms),
		"source_id":   sourceID,
		"namespace":   namespace,
	})
}

// =========================
// Envoy handlers
// =========================

// handleEnvoyClusters returns Envoy cluster health summaries.
// GET /api/v1/envoy/{sourceID}/clusters
func (s *Server) handleEnvoyClusters(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	clusters, err := s.accessor.EnvoyClusters(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "envoy", "clusters", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "envoy") {
			return
		}
		writeInternalError(w, err, "envoy clusters")
		return
	}

	if clusters == nil {
		clusters = []envoyadapter.ClusterStatus{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"clusters":  clusters,
		"count":     len(clusters),
		"source_id": sourceID,
	})
}

// handleEnvoyConfigDump returns Envoy config dump, optionally filtered by section.
// GET /api/v1/envoy/{sourceID}/config?section=
func (s *Server) handleEnvoyConfigDump(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	section := r.URL.Query().Get("section")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	dump, err := s.accessor.EnvoyConfigDump(r.Context(), principal, sourceID, section)
	s.services.Metrics.RecordAdapterCall(r.Context(), "envoy", "config", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "envoy") {
			return
		}
		writeInternalError(w, err, "envoy config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config":    dump,
		"section":   section,
		"source_id": sourceID,
	})
}

// handleEnvoyStats returns Envoy statistics.
// GET /api/v1/envoy/{sourceID}/stats?filter=
func (s *Server) handleEnvoyStats(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	filter := r.URL.Query().Get("filter")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	stats, err := s.accessor.EnvoyStats(r.Context(), principal, sourceID, filter)
	s.services.Metrics.RecordAdapterCall(r.Context(), "envoy", "stats", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "envoy") {
			return
		}
		writeInternalError(w, err, "envoy stats")
		return
	}

	if stats == nil {
		stats = []envoyadapter.Stat{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats":     stats,
		"count":     len(stats),
		"filter":    filter,
		"source_id": sourceID,
	})
}

// registerNetworkingRoutes registers NGINX and Envoy routes.
func (s *Server) registerNetworkingRoutes(mux *http.ServeMux, prefix string) {
	// NGINX Ingress Controller routes.
	mux.HandleFunc(fmt.Sprintf("GET %s/nginx/{sourceID}/ingresses", prefix), s.handleNginxIngresses)
	mux.HandleFunc(fmt.Sprintf("GET %s/nginx/{sourceID}/status", prefix), s.handleNginxStatus)
	mux.HandleFunc(fmt.Sprintf("GET %s/nginx/{sourceID}/config", prefix), s.handleNginxConfigMaps)
	// Envoy admin API routes.
	mux.HandleFunc(fmt.Sprintf("GET %s/envoy/{sourceID}/clusters", prefix), s.handleEnvoyClusters)
	mux.HandleFunc(fmt.Sprintf("GET %s/envoy/{sourceID}/config", prefix), s.handleEnvoyConfigDump)
	mux.HandleFunc(fmt.Sprintf("GET %s/envoy/{sourceID}/stats", prefix), s.handleEnvoyStats)
}
