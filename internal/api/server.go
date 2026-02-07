package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// Server handles HTTP API requests for joecored
type Server struct {
	// TODO: Add dependencies (core services, core agent, etc.)
}

// New creates a new API server
func New() *Server {
	return &Server{}
}

// RegisterRoutes registers all API routes on the given mux
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Status
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)

	// Graph (placeholder)
	mux.HandleFunc("GET /api/v1/graph/query", s.handleNotImplemented)
	mux.HandleFunc("GET /api/v1/graph/related/{nodeID}", s.handleNotImplemented)
	mux.HandleFunc("GET /api/v1/graph/summary", s.handleNotImplemented)

	// Sources (placeholder)
	mux.HandleFunc("GET /api/v1/sources", s.handleNotImplemented)
	mux.HandleFunc("POST /api/v1/sources", s.handleNotImplemented)

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
		"status":  "ok",
		"version": "0.1.0",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleNotImplemented(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "not implemented",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
