package api

import (
	"encoding/json"
	"io"
	"net/http"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/store"
)

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.services.Store.Sources.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if sources == nil {
		sources = []*store.Source{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sources": sources,
		"count":   len(sources),
	})
}

// createSourceRequest is the JSON body for POST /api/v1/sources.
type createSourceRequest struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	var req createSourceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if req.ID == "" || req.Type == "" || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id, type, and name are required"})
		return
	}

	// Check if source already exists
	existing, err := s.services.Store.Sources.Get(r.Context(), req.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "source already exists"})
		return
	}

	source := &store.Source{
		ID:     req.ID,
		Type:   req.Type,
		Name:   req.Name,
		Config: req.Config,
	}

	// Try to connect the adapter before saving
	if req.Type == "kubernetes" {
		adapter := k8s.New()
		if err := adapter.Connect(*source); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "failed to connect to cluster: " + err.Error(),
			})
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	} else if req.Type == "git" {
		adapter := gitadapter.New()
		if err := adapter.Connect(*source); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "failed to connect to git repo: " + err.Error(),
			})
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	}

	if err := s.services.Store.Sources.Create(r.Context(), source); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, source)
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing source id"})
		return
	}

	source, err := s.services.Store.Sources.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if source == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}

	writeJSON(w, http.StatusOK, source)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing source id"})
		return
	}

	source, err := s.services.Store.Sources.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if source == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found"})
		return
	}

	// Disconnect and unregister adapter
	if err := s.services.Adapters.Unregister(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := s.services.Store.Sources.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
