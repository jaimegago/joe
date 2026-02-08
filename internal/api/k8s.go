package api

import (
	"net/http"
	"strconv"

	"github.com/jaimegago/joe/internal/adapters/k8s"
)

func (s *Server) handleK8sListResources(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not a kubernetes adapter"})
		return
	}

	resource := r.URL.Query().Get("resource")
	if resource == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required query parameter 'resource'"})
		return
	}

	namespace := r.URL.Query().Get("namespace")

	items, err := k8sAdapter.ListResources(r.Context(), resource, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not a kubernetes adapter"})
		return
	}

	resource := r.PathValue("resource")
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	obj, err := k8sAdapter.GetResource(r.Context(), resource, namespace, name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"resource":  obj.Object,
		"source_id": sourceID,
	})
}

func (s *Server) handleK8sGetLogs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return
	}

	k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not a kubernetes adapter"})
		return
	}

	namespace := r.PathValue("namespace")
	pod := r.PathValue("pod")
	container := r.URL.Query().Get("container")

	tailLines := 100
	if t := r.URL.Query().Get("tail"); t != "" {
		parsed, err := strconv.Atoi(t)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tail must be a positive integer"})
			return
		}
		tailLines = parsed
	}

	logs, err := k8sAdapter.GetPodLogs(r.Context(), namespace, pod, container, tailLines)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"logs":      logs,
		"pod":       pod,
		"namespace": namespace,
		"source_id": sourceID,
	})
}
