package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/audit"
)

// The session audit trail (D-0171): the acts a session's agent loop took
// against managed systems, read back in the order they were written.
//
// It is the audit log's only read route, and it is a read in the strict
// sense: it writes no audit row of its own, so asking for a trail never
// changes one. Nothing on this surface — and nothing anywhere in the API —
// updates or deletes an entry; the handler holds an audit.TrailReader, which
// declares one read and no write.
//
// Authorization is the session's own read posture, unchanged: sessions are
// readable by every authenticated principal (DESIGN-CHAT-SESSIONS.md §12.7,
// team-wide read), the route sits behind the same edge authentication as
// GET /sessions/{id}/messages, and it discloses less than that route does —
// the messages carry every tool call with its arguments and its result, and
// an entry carries the operation, the model's stated reason and the trigger.

// auditTrailResponse is the body of GET /sessions/{id}/audit-trail.
type auditTrailResponse struct {
	SessionID string `json:"session_id"`
	// Entries are the session's acts in write order. Always an array: a
	// session that took no act against a managed system returns `[]`.
	Entries []audit.TrailEntry `json:"entries"`
	Count   int                `json:"count"`
}

// registerAuditTrailRoutes registers the session audit-trail read. The path
// is a GET sub-resource of the session, a sibling of .../messages; it is a
// distinct, longer path than GET /sessions/{id}, so there is no collision.
func (s *Server) registerAuditTrailRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}/audit-trail", prefix), s.handleGetSessionAuditTrail)
}

// handleGetSessionAuditTrail serves one session's audit trail.
//
// A session that does not exist is 404, never an empty trail: an empty trail
// is a statement that the session took no act, and Joe cannot make that
// statement about a session it does not hold. A failed read is 500 for the
// same reason — the three answers (a trail, no such session, a failure) are
// kept apart so a caller never reads a failure as "nothing was recorded".
func (s *Server) handleGetSessionAuditTrail(w http.ResponseWriter, r *http.Request) {
	if s.services.AuditTrail == nil || s.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "audit trail not available")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	sess, err := s.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	entries, err := s.services.AuditTrail.SessionTrail(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "read session audit trail")
		return
	}

	writeJSON(w, http.StatusOK, auditTrailResponse{
		SessionID: sessionID,
		Entries:   entries,
		Count:     len(entries),
	})
}
