package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/rbac"
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
	// Re-homed under /sessions in B005 (§12.8): the legacy /agent-sessions
	// namespace was removed. POST requires an authenticated principal and derives
	// the author from context (the spoof-closed accountability fix); GET is a
	// team-wide read.
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/findings", prefix), h.post)
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}/findings", prefix), h.list)
}

type postFindingRequest struct {
	ID              string `json:"id,omitempty"`
	SourceSessionID string `json:"source_session_id"`
	// AuthorPrincipal is intentionally NOT read from the request body (B005,
	// mirroring the B002 context-derived creator fix). The author is the
	// context-resolved authenticated principal so it cannot be spoofed. A field
	// supplied here is ignored.
	Body                             string  `json:"body"`
	ReferencedInvestigationSessionID *string `json:"referenced_investigation_session_id,omitempty"`
}

func (h *findingsHandler) post(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		writeBadRequest(w, nil, "post finding", "missing target session id")
		return
	}
	// Author is the context-derived authenticated principal (§12.1 accountability),
	// never client-supplied. A request with no resolvable principal cannot post.
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}
	var req postFindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "post finding", "invalid request body")
		return
	}
	if req.SourceSessionID == "" || req.Body == "" {
		writeBadRequest(w, nil, "post finding",
			"source_session_id and body are required")
		return
	}

	f := findings.Finding{
		ID:                               req.ID,
		SourceSessionID:                  req.SourceSessionID,
		TargetSessionID:                  targetID,
		AuthorPrincipal:                  string(principal),
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
