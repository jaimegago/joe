package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jaimegago/joe/internal/constants"
	"github.com/jaimegago/joe/internal/rbac"
)

func (s *Server) handleK8sListResources(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	resource := r.URL.Query().Get("resource")
	if resource == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'resource'", map[string]any{
			"param": "resource",
		})
		return
	}

	namespace := r.URL.Query().Get("namespace")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	items, err := s.accessor.K8sListResources(r.Context(), principal, sourceID, resource, namespace)
	s.services.Metrics.RecordAdapterCall(r.Context(), "k8s", "list_resources", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "k8s") {
			return
		}
		writeInternalError(w, err, "k8s list resources")
		return
	}

	// Convert unstructured items to plain maps for JSON serialization
	resources := make([]map[string]any, len(items))
	for i, item := range items {
		resources[i] = item.Object
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"resources": resources,
		"count":     len(resources),
		"source_id": sourceID,
	})
}

func (s *Server) handleK8sGetResource(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	resource := r.PathValue("resource")
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	obj, err := s.accessor.K8sGetResource(r.Context(), principal, sourceID, resource, namespace, name)
	s.services.Metrics.RecordAdapterCall(r.Context(), "k8s", "get_resource", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "k8s") {
			return
		}
		writeInternalError(w, err, "k8s get resource")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"resource":  obj.Object,
		"source_id": sourceID,
	})
}

func (s *Server) handleK8sGetLogs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	namespace := r.PathValue("namespace")
	pod := r.PathValue("pod")
	container := r.URL.Query().Get("container")

	tailLines := constants.DefaultK8sTailLines
	if t := r.URL.Query().Get("tail"); t != "" {
		parsed, err := strconv.Atoi(t)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "tail must be a positive integer", map[string]any{
				"param": "tail",
				"value": t,
			})
			return
		}
		tailLines = parsed
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	logs, err := s.accessor.K8sGetPodLogs(r.Context(), principal, sourceID, namespace, pod, container, tailLines)
	s.services.Metrics.RecordAdapterCall(r.Context(), "k8s", "get_pod_logs", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "k8s") {
			return
		}
		writeInternalError(w, err, "k8s get logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"logs":      logs,
		"pod":       pod,
		"namespace": namespace,
		"source_id": sourceID,
	})
}
