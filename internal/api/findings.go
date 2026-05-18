package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/findings"
)

// findingsHandler exposes the §A4 cross-session attribution HTTP endpoints.
// Findings are scoped to a target session: POST /sessions/{id}/findings
// creates a finding with that session as the target; GET returns all
// findings posted into that session's timeline.
type findingsHandler struct {
	repo findings.Repository
}

func (s *Server) registerFindingsRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.Findings == nil {
		return
	}
	h := &findingsHandler{repo: s.services.Findings}
	// Mounted under /agent-sessions for parity with sessions.go. See the
	// namespace explainer in sessions.go.
	mux.HandleFunc(fmt.Sprintf("POST %s/agent-sessions/{id}/findings", prefix), h.post)
	mux.HandleFunc(fmt.Sprintf("GET %s/agent-sessions/{id}/findings", prefix), h.list)
}

type postFindingRequest struct {
	ID                               string  `json:"id,omitempty"`
	SourceSessionID                  string  `json:"source_session_id"`
	AuthorPrincipal                  string  `json:"author_principal"`
	Body                             string  `json:"body"`
	ReferencedInvestigationSessionID *string `json:"referenced_investigation_session_id,omitempty"`
}

func (h *findingsHandler) post(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		writeBadRequest(w, nil, "post finding", "missing target session id")
		return
	}
	var req postFindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "post finding", "invalid request body")
		return
	}
	if req.SourceSessionID == "" || req.AuthorPrincipal == "" || req.Body == "" {
		writeBadRequest(w, nil, "post finding",
			"source_session_id, author_principal, and body are required")
		return
	}

	f := findings.Finding{
		ID:                               req.ID,
		SourceSessionID:                  req.SourceSessionID,
		TargetSessionID:                  targetID,
		AuthorPrincipal:                  req.AuthorPrincipal,
		Body:                             req.Body,
		ReferencedInvestigationSessionID: req.ReferencedInvestigationSessionID,
	}
	if f.ID == "" {
		f.ID = uuid.NewString()
	}

	created, err := h.repo.PostFinding(r.Context(), f)
	if err != nil {
		writeInternalError(w, err, "post finding")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *findingsHandler) list(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		writeBadRequest(w, nil, "list findings", "missing target session id")
		return
	}
	items, err := h.repo.ListFindingsForTarget(r.Context(), targetID)
	if err != nil {
		writeInternalError(w, err, "list findings")
		return
	}
	if items == nil {
		items = []findings.Finding{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"findings": items,
		"count":    len(items),
	})
}
