package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// sessionsHandler exposes the session-model HTTP CRUD — Phase 1 Change 4.
// See docs/PHASE-1-DECOMPOSITION.md.
//
// Path namespace: /api/v1/agent-sessions (not /api/v1/sessions).
// The decomposition specifies /api/v1/sessions, but the existing webui
// handler (internal/api/webui.go) registers GET/POST /api/v1/sessions
// against the legacy migration-001 `sessions` table — and the Phase 1
// scope-isolation rule explicitly forbids modifying webui.go. The
// parallel /agent-sessions namespace matches the Go type name
// (sessionmodel.AgentSession) and lets the legacy webui route survive
// untouched. The post-Phase-1 webui migration will collapse the two.
//
// §5b-3 team-global: sessions are team-scoped and readable across the
// team (governed by RBAC read-authorization upstream). No handler in
// this file filters by created_by_principal. A future contributor adding
// such a filter would violate §5b-3 and break TestSessionsAPI_TeamGlobal.
type sessionsHandler struct {
	repo sessionmodel.Repository
}

func (s *Server) registerSessionModelRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.SessionModel == nil {
		return
	}
	h := &sessionsHandler{repo: s.services.SessionModel}
	mux.HandleFunc(fmt.Sprintf("POST %s/agent-sessions", prefix), h.create)
	mux.HandleFunc(fmt.Sprintf("GET %s/agent-sessions", prefix), h.list)
	mux.HandleFunc(fmt.Sprintf("GET %s/agent-sessions/{id}", prefix), h.get)
	// Per-row DELETE in Phase 1 Change 4. The schema-level ON DELETE
	// CASCADE shipped in migrations 009/010/011 carries §5b-5 expunge
	// downward. Change 11 layers the incident-cascade contract on top.
	mux.HandleFunc(fmt.Sprintf("DELETE %s/agent-sessions/{id}", prefix), h.delete)
}

type createSessionRequest struct {
	ID            string                      `json:"id,omitempty"`
	Type          sessionmodel.SessionType    `json:"type"`
	IncidentState *sessionmodel.IncidentState `json:"incident_state,omitempty"`
	// NOTE: creator_principal is intentionally NOT a request field. The creator
	// is the context-resolved authenticated principal (§12.1) and is never
	// accepted from the request body — that closes the spoofable-creator defect
	// by construction.
	LinkedIncidentID *string `json:"linked_incident_id,omitempty"`
	RetentionClass   *string `json:"retention_class,omitempty"`
}

func (h *sessionsHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "create session", "invalid request body")
		return
	}

	if !isValidSessionType(req.Type) {
		writeBadRequest(w, nil, "create session",
			"type is required and must be one of: default, incident")
		return
	}
	// Creator is the context-derived authenticated principal (§12.1), never
	// client-supplied. A request with no resolvable principal cannot create.
	creatorPrincipal := string(rbac.PrincipalFromContext(r.Context()))
	if creatorPrincipal == "" {
		writeBadRequest(w, nil, "create session", "no authenticated principal in context")
		return
	}
	// Incident sessions must carry an incident_state; non-incident must not.
	// The schema CHECK enforces this too, but a clean 400 beats a CHECK-
	// constraint surface error.
	if req.Type == sessionmodel.SessionTypeIncident {
		if req.IncidentState == nil || !isValidIncidentState(*req.IncidentState) {
			writeBadRequest(w, nil, "create session",
				"incident sessions require incident_state in {declared, being_worked, believed_mitigated, resolved, reviewed}")
			return
		}
	} else if req.IncidentState != nil {
		writeBadRequest(w, nil, "create session",
			"incident_state is only valid for type=incident")
		return
	}

	sess := sessionmodel.AgentSession{
		ID:               req.ID,
		Type:             req.Type,
		IncidentState:    req.IncidentState,
		CreatorPrincipal: creatorPrincipal,
		LinkedIncidentID: req.LinkedIncidentID,
		RetentionClass:   req.RetentionClass,
	}
	if sess.ID == "" {
		sess.ID = uuid.NewString()
	}
	sess.CreatedAt = time.Now().UTC()
	sess.LastActivityAt = sess.CreatedAt

	created, err := h.repo.CreateSession(r.Context(), sess)
	if err != nil {
		writeInternalError(w, err, "create session")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *sessionsHandler) list(w http.ResponseWriter, r *http.Request) {
	typeFilter := r.URL.Query().Get("type")
	var (
		sessions []sessionmodel.AgentSession
		err      error
	)
	if typeFilter != "" {
		if !isValidSessionType(sessionmodel.SessionType(typeFilter)) {
			writeBadRequest(w, nil, "list sessions",
				"type filter must be one of: default, incident")
			return
		}
		sessions, err = h.repo.ListSessionsByType(r.Context(), sessionmodel.SessionType(typeFilter))
	} else {
		sessions, err = h.repo.ListSessions(r.Context())
	}
	if err != nil {
		writeInternalError(w, err, "list sessions")
		return
	}
	if sessions == nil {
		sessions = []sessionmodel.AgentSession{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

func (h *sessionsHandler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, nil, "get session", "missing session id")
		return
	}
	sess, err := h.repo.GetSession(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found", map[string]any{"id": id})
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *sessionsHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, nil, "delete session", "missing session id")
		return
	}
	// Phase 1 Change 4 shipped this handler; Change 11 added the
	// integration tests that prove the §5b-5 expunge cascade end-to-end.
	// The handler runs ONE SQL DELETE. The schema's ON DELETE CASCADE
	// FKs (linked investigations via the self-FK; runs/findings/etc.
	// via their own FKs to agent_sessions and agent_runs) do the rest.
	// If this ever grows a gather/fan-out step, §6-C has failed and the
	// schema is the place to fix it — see internal/sessionmodel/
	// cascade_schema_test.go and internal/runmodel/cascade_schema_test.go.
	if err := h.repo.DeleteSession(r.Context(), id); err != nil {
		writeInternalError(w, err, "delete session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

func isValidSessionType(t sessionmodel.SessionType) bool {
	switch t {
	case sessionmodel.SessionTypeDefault,
		sessionmodel.SessionTypeIncident:
		return true
	}
	return false
}

func isValidIncidentState(s sessionmodel.IncidentState) bool {
	switch s {
	case sessionmodel.IncidentStateDeclared,
		sessionmodel.IncidentStateBeingWorked,
		sessionmodel.IncidentStateBelievedMitigated,
		sessionmodel.IncidentStateResolved,
		sessionmodel.IncidentStateReviewed:
		return true
	}
	return false
}
