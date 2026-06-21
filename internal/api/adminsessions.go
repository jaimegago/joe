package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionauthz"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// adminsessions.go is the admin session-governance namespace
// (/api/v1/admin/sessions, DESIGN-CHAT-SESSIONS.md §12.8, ledger node B006). It
// is the cross-tenant operator console for sessions: an admin acts on ANY
// session regardless of creator. It sits alongside the component-governance
// admin surface (admin.go) and follows the same shape.
//
// DEFENSE IN DEPTH (§12.8). Every governing route here requires BOTH:
//  1. the admin route prefix's gate — (*Server).requireAdmin — which honours the
//     auth-disabled permit convention (RBAC off ⇒ permit), AND
//  2. an admin RELATIONSHIP resolved through the ADMIN session seam instance —
//     (*Server).sessionAccessAdmin — which uses the real D-0011 checker and
//     denies when RBAC is off.
//
// The two are deliberately asymmetric under RBAC-off (gate permits, seam denies);
// their intersection is the safe posture — see newAdminSessionAuthz in
// sessiongate.go. A per-user /api/v1/sessions route can NEVER reach this seam
// instance (it holds only the always-false per-user instance), so the admin
// relationship is structurally unreachable outside this prefix.
//
// READS vs GOVERN.
//   - The cross-tenant READS (list-all, get, get-messages) are ordinary reads
//     (§12.10: admin content viewing is an ordinary content read, no privacy
//     gate, no special audit verb). They are gated by requireAdmin only and write
//     NO audit row.
//   - The GOVERN actions (purge, archive, restore-archive, configure_retention)
//     require BOTH gate + admin relationship and write ONE KindAdminAccess audit
//     row naming the operator and the target session (§12.2 / §12.5).
//
// EFFECTS DEFERRED TO B007. The store effect of every govern action below does
// not exist yet — there is no soft-delete/purge pipeline, no archive provider,
// no retention-policy store (B002 added the lifecycle COLUMNS but no transition
// methods). B006 wires, authorizes, and AUDITS the governance decision, then
// reports the effect as pending with 501. It does NOT silently succeed and does
// NOT co-opt the raw hard-delete (DeleteSession) as "purge" — the designed purge
// (manifest, sever linked children, cascade) is B007's. The list-all-trash and
// retention-policy GET reads likewise have no backing store yet and report 501.
type adminSessionsHandler struct {
	server *Server
}

func (s *Server) registerAdminSessionRoutes(mux *http.ServeMux, prefix string) {
	h := &adminSessionsHandler{server: s}

	// Cross-tenant reads (requireAdmin only; team-wide read, no audit).
	mux.HandleFunc("GET "+prefix+"/admin/sessions", h.handleListAll)
	mux.HandleFunc("GET "+prefix+"/admin/sessions/{id}", h.handleGet)
	mux.HandleFunc("GET "+prefix+"/admin/sessions/{id}/messages", h.handleGetMessages)

	// Govern actions (requireAdmin + admin seam + audit; effects deferred B007).
	mux.HandleFunc("POST "+prefix+"/admin/sessions/{id}/purge", h.handlePurge)
	mux.HandleFunc("POST "+prefix+"/admin/sessions/{id}/archive", h.handleArchive)
	mux.HandleFunc("POST "+prefix+"/admin/sessions/{id}/restore-archive", h.handleUnarchive)

	// Collection-level deferred surfaces. The literal segments (trash,
	// retention-policy) take precedence over the {id} wildcard in Go's ServeMux,
	// so they coexist with GET /admin/sessions/{id}.
	mux.HandleFunc("GET "+prefix+"/admin/sessions/trash", h.handleListAllTrash)
	mux.HandleFunc("GET "+prefix+"/admin/sessions/retention-policy", h.handleGetRetentionPolicy)
	mux.HandleFunc("PUT "+prefix+"/admin/sessions/retention-policy", h.handlePutRetentionPolicy)
}

// handleListAll is the cross-tenant list (§12.8), filterable by principal, type,
// and state. requireAdmin only — a team-wide read surfaced on the admin console.
func (h *adminSessionsHandler) handleListAll(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.server.services.SessionModel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "count": 0})
		return
	}

	q := r.URL.Query()
	principalFilter := q.Get("principal")
	typeFilter := q.Get("type")
	// state is accepted for forward-compat with §12.8 ("filter by ... state");
	// lifecycle state is timestamp-derived and the transitions land in B007, so
	// a state filter has nothing to match yet. Reported, not silently dropped.
	stateFilter := q.Get("state")

	var (
		all []sessionmodel.AgentSession
		err error
	)
	if typeFilter != "" {
		all, err = h.server.services.SessionModel.ListSessionsByType(r.Context(), sessionmodel.SessionType(typeFilter))
	} else {
		all, err = h.server.services.SessionModel.ListSessions(r.Context())
	}
	if err != nil {
		writeInternalError(w, err, "admin list sessions")
		return
	}

	out := make([]webUISession, 0, len(all))
	for i := range all {
		if principalFilter != "" && all[i].CreatorPrincipal != principalFilter {
			continue
		}
		out = append(out, sessionToWebUI(all[i], 0))
	}

	resp := map[string]any{"sessions": out, "count": len(out)}
	if stateFilter != "" {
		resp["state_filter_pending"] = "lifecycle-state filtering is deferred to B007 (timestamp-driven trash/archive)"
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGet is the cross-tenant single-session read (§12.8). Ordinary read.
func (h *adminSessionsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	sess := h.lookup(w, r)
	if sess == nil {
		return
	}
	writeJSON(w, http.StatusOK, sessionToWebUI(*sess, 0))
}

// handleGetMessages is the cross-tenant transcript read (§12.8). Ordinary read —
// no privacy gate and no special audit verb (§12.10).
func (h *adminSessionsHandler) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	sess := h.lookup(w, r)
	if sess == nil {
		return
	}
	messages, err := h.server.services.SessionModel.ListChatMessages(r.Context(), sess.ID)
	if err != nil {
		writeInternalError(w, err, "admin get session messages")
		return
	}
	out := make([]webUIMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, messageToWebUI(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "count": len(out)})
}

// handlePurge is the admin purge govern route (§12.5 manifest-with-hard-stop).
// Effect deferred to B007; the decision is authorized + audited here.
func (h *adminSessionsHandler) handlePurge(w http.ResponseWriter, r *http.Request) {
	h.governDeferred(w, r, sessionauthz.ActionPurge, audit.ActionSessionPurge,
		"purge (trash-and-empty, sever linked children, manifest-with-hard-stop)")
}

// handleArchive is the admin archive govern route (§12.6 provider seam).
func (h *adminSessionsHandler) handleArchive(w http.ResponseWriter, r *http.Request) {
	h.governDeferred(w, r, sessionauthz.ActionArchive, audit.ActionSessionArchive,
		"archive (move transcript to cold storage)")
}

// handleUnarchive is the admin restore-archive govern route (§12.6).
func (h *adminSessionsHandler) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	h.governDeferred(w, r, sessionauthz.ActionUnarchive, audit.ActionSessionUnarchive,
		"restore-archive (rehydrate transcript from cold storage)")
}

// handleListAllTrash is the cross-tenant all-trash list (§12.8). The trash store
// (timestamp-driven soft-delete) lands in B007, so there is nothing to list yet.
func (h *adminSessionsHandler) handleListAllTrash(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	writePendingB007(w, "admin all-trash list", "trash querying (timestamp-driven soft-delete)")
}

// handleGetRetentionPolicy reads the admin retention policy (§12.5). The policy
// store lands in B007; there is no policy row to return yet.
func (h *adminSessionsHandler) handleGetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	writePendingB007(w, "get retention policy", "retention-policy store")
}

// handlePutRetentionPolicy is the configure_retention govern route (§12.5
// inactivity-window / trash-grace / terminal-action). Effect (policy store)
// deferred to B007; the decision is authorized + audited here.
func (h *adminSessionsHandler) handlePutRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	principal, gated := h.server.requireAdmin(w, r)
	if gated {
		return
	}
	// configure_retention is policy-scoped, not session-scoped: there is no
	// session id. Resolve the admin relationship with an empty session id (the
	// seam resolves admin purely from the principal's capability) so the BOTH
	// requirement (gate + admin relationship) still holds.
	decision, err := h.server.sessionAccessAdmin(r.Context(), principal, "", sessionauthz.ActionConfigureRetention)
	if err != nil {
		writeInternalError(w, err, "authorize configure_retention")
		return
	}
	if !decision.Allowed {
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied: admin capability required")
		return
	}
	if err := h.auditGovernDecision(r.Context(), principal, audit.ActionSessionConfigureRetention, ""); err != nil {
		writeInternalError(w, err, "configure_retention audit")
		return
	}
	writePendingB007(w, "configure retention policy", "retention-policy store")
}

// governDeferred is the shared body of the session-scoped admin-govern routes
// whose store effect is deferred to B007. It enforces BOTH conditions (the
// requireAdmin prefix gate already ran via the per-handler call below; here it
// resolves the admin RELATIONSHIP through the admin seam), writes the one
// governance-decision audit row, then reports the effect pending with 501. It
// never silently succeeds.
func (h *adminSessionsHandler) governDeferred(w http.ResponseWriter, r *http.Request, action sessionauthz.Action, auditAction, effect string) {
	principal, gated := h.server.requireAdmin(w, r)
	if gated {
		return
	}
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	// BOTH-conditions: requireAdmin (prefix) passed above; now the admin
	// relationship via the admin seam. A non-admin reaching here (only possible
	// when RBAC is off and the gate permitted) is denied by the seam.
	decision, err := h.server.sessionAccessAdmin(r.Context(), principal, sessionID, action)
	if err != nil {
		writeInternalError(w, err, "authorize admin session govern")
		return
	}
	if !decision.Allowed {
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied: admin capability required")
		return
	}
	// The session must exist for the govern decision to name a real target.
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "admin govern get session")
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	// Audit the authorized governance DECISION (effect pending). Fail-closed:
	// an audit failure aborts the request rather than reporting a governed action
	// that left no trail.
	if err := h.auditGovernDecision(r.Context(), principal, auditAction, sessionID); err != nil {
		writeInternalError(w, err, "admin govern audit")
		return
	}
	writePendingB007(w, "admin "+string(action), effect)
}

// lookup resolves the path-named session for the admin read routes, writing the
// 400/404/503 responses. Returns nil when a response was already written.
func (h *adminSessionsHandler) lookup(w http.ResponseWriter, r *http.Request) *sessionmodel.AgentSession {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return nil
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return nil
	}
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "admin get session")
		return nil
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return nil
	}
	return sess
}

// auditGovernDecision writes one KindAdminAccess / decision=allow row for an
// authorized admin-govern decision. Because the store EFFECT is deferred to B007
// there is no mutation to couple the row to yet, so it is written via the plain
// Insert path (not InsertTx); when B007 supplies the real transitions the row
// moves into the effect's transaction via mutateWithAudit. A nil audit repository
// (dev/local without the table) is the same no-op carve-out the rest of the admin
// surface uses; fail-closed on a real write error so the caller aborts.
func (h *adminSessionsHandler) auditGovernDecision(ctx context.Context, principal rbac.Principal, action, sessionID string) error {
	if h.server.services.Audit == nil {
		return nil
	}
	blob, _ := json.Marshal(audit.Details{Target: "session:" + sessionID})
	err := h.server.services.Audit.Insert(ctx, audit.Event{
		Principal: string(principal),
		Action:    action,
		Decision:  audit.DecisionAllow,
		Reason:    "effect_pending_b007",
		Kind:      audit.KindAdminAccess,
		Context:   string(blob),
	})
	return audit.FailurePosture(ctx, action, err, "adminsessions:"+action, audit.FailClosed)
}

// writePendingB007 reports an authorized-and-audited govern decision (or a read
// with no backing store yet) whose store effect is deferred to ledger node B007.
// 501 Not Implemented is deliberate: the route exists, the decision was made and
// recorded, but the effect is not implemented — never a 2xx that would read as a
// completed governance action.
func writePendingB007(w http.ResponseWriter, op, effect string) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"status":      "effect_pending",
		"deferred_to": "B007",
		"operation":   op,
		"effect":      effect,
		"message":     op + ": authorized and audited; store effect is deferred to B007 (retention pipeline + archive provider)",
	})
}
