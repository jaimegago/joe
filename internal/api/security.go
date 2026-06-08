package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/rbac"
)

// registerSecurityRoutes registers security and runtime adapter routes (Falco).
func (s *Server) registerSecurityRoutes(mux *http.ServeMux, prefix string) {
	h := &securityHandler{server: s}
	// Falco
	mux.HandleFunc(fmt.Sprintf("GET %s/falco/{componentID}/events", prefix), h.handleFalcoEvents)
	mux.HandleFunc(fmt.Sprintf("GET %s/falco/{componentID}/rules", prefix), h.handleFalcoRules)
}

// securityHandler delegates to Server security methods.
type securityHandler struct{ server *Server }

func (h *securityHandler) handleFalcoEvents(w http.ResponseWriter, r *http.Request) {
	h.server.handleFalcoEvents(w, r)
}

func (h *securityHandler) handleFalcoRules(w http.ResponseWriter, r *http.Request) {
	h.server.handleFalcoRules(w, r)
}

// --- Falco handlers ---

// handleFalcoEvents lists recent Falco runtime security events.
// GET /api/v1/falco/{componentID}/events?priority=<p>&source=<s>&rule=<r>&limit=<n>
func (s *Server) handleFalcoEvents(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	priority := r.URL.Query().Get("priority")
	source := r.URL.Query().Get("source")
	rule := r.URL.Query().Get("rule")

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	events, err := s.accessor.FalcoListEvents(r.Context(), principal, sourceID, priority, source, rule, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "falco", "list_events", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Falco") {
			return
		}
		writeInternalError(w, err, "falco list events")
		return
	}

	if events == nil {
		events = []falcoadapter.Event{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events":       events,
		"count":        len(events),
		"component_id": sourceID,
	})
}

// handleFalcoRules lists Falco rules derived from recent events.
// GET /api/v1/falco/{componentID}/rules
func (s *Server) handleFalcoRules(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	rules, err := s.accessor.FalcoListRules(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "falco", "list_rules", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Falco") {
			return
		}
		writeInternalError(w, err, "falco list rules")
		return
	}

	if rules == nil {
		rules = []falcoadapter.Rule{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rules":        rules,
		"count":        len(rules),
		"component_id": sourceID,
	})
}
