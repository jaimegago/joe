package api

import (
	"fmt"
	"net/http"
	"time"

	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
)

// ---- Adapter lookup helpers ----

func (s *Server) getNginxAdapter(sourceID string) (nginxadapter.NginxAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	a, ok := adapter.(nginxadapter.NginxAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: nginx-ingress", errInvalidSourceType)
	}
	return a, nil
}

func (s *Server) getEnvoyAdapter(sourceID string) (envoyadapter.EnvoyAdapter, error) {
	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		return nil, err
	}
	a, ok := adapter.(envoyadapter.EnvoyAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: envoy", errInvalidSourceType)
	}
	return a, nil
}

// =========================
// NGINX handlers
// =========================

// handleNginxIngresses lists Ingress resources.
// GET /api/v1/nginx/{sourceID}/ingresses?namespace=
func (s *Server) handleNginxIngresses(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	namespace := r.URL.Query().Get("namespace")

	a, err := s.getNginxAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "nginx-ingress") {
		return
	}

	start := time.Now()
	ingresses, err := a.ListIngresses(r.Context(), namespace)
	s.services.Metrics.RecordAdapterCall(r.Context(), "nginx", "ingresses", time.Since(start), err)
	if err != nil {
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

	a, err := s.getNginxAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "nginx-ingress") {
		return
	}

	start := time.Now()
	status, err := a.GetNginxStatus(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "nginx", "status", time.Since(start), err)
	if err != nil {
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

	a, err := s.getNginxAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "nginx-ingress") {
		return
	}

	start := time.Now()
	cms, err := a.ListConfigMaps(r.Context(), namespace)
	s.services.Metrics.RecordAdapterCall(r.Context(), "nginx", "configmaps", time.Since(start), err)
	if err != nil {
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

	a, err := s.getEnvoyAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "envoy") {
		return
	}

	start := time.Now()
	clusters, err := a.Clusters(r.Context())
	s.services.Metrics.RecordAdapterCall(r.Context(), "envoy", "clusters", time.Since(start), err)
	if err != nil {
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

	a, err := s.getEnvoyAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "envoy") {
		return
	}

	start := time.Now()
	dump, err := a.ConfigDump(r.Context(), section)
	s.services.Metrics.RecordAdapterCall(r.Context(), "envoy", "config", time.Since(start), err)
	if err != nil {
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

	a, err := s.getEnvoyAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "envoy") {
		return
	}

	start := time.Now()
	stats, err := a.Stats(r.Context(), filter)
	s.services.Metrics.RecordAdapterCall(r.Context(), "envoy", "stats", time.Since(start), err)
	if err != nil {
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
