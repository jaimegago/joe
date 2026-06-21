package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionarchive"
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
// EFFECTS (B007a). B006 wired, authorized, and AUDITED every govern decision but
// reported the effect pending with 501. B007a fills the synchronous route-backing
// effects:
//   - purge: the §12.5 manifest-with-hard-stop expunge. PurgeSessionTx cascades
//     the transcript + captain bindings and severs linked children
//     (ON DELETE SET NULL), committed atomically with its audit row.
//   - configure_retention / retention-policy get: the §12.5 retention-policy
//     store (migration 026), real read and write.
//   - list-all-trash: real timestamp-driven trash query.
//
// ARCHIVE EFFECTS (B007c). Archive and restore-archive now run real
// provider-backed effects (§12.6) through services.SessionArchive: archive reads
// the live transcript, writes the versioned self-contained artifact, then stamps
// archived_at/archived_by/archive_ref AND removes the hot transcript rows (the
// move to cold storage) coupled to its audit row in ONE transaction; restore
// parses the artifact (refusing an unknown schema version), clears the archive
// columns, and rebuilds the hot transcript rows in ONE audited transaction. The
// B006 authorize+audit-decision+501 posture (governDeferred / auditGovernDecision
// / writePendingB007) is RETIRED — those two routes were its last callers.
type adminSessionsHandler struct {
	server *Server
}

func (s *Server) registerAdminSessionRoutes(mux *http.ServeMux, prefix string) {
	h := &adminSessionsHandler{server: s}

	// Cross-tenant reads (requireAdmin only; team-wide read, no audit).
	mux.HandleFunc("GET "+prefix+"/admin/sessions", h.handleListAll)
	mux.HandleFunc("GET "+prefix+"/admin/sessions/{id}", h.handleGet)
	mux.HandleFunc("GET "+prefix+"/admin/sessions/{id}/messages", h.handleGetMessages)

	// Govern actions (requireAdmin + admin seam + audit). purge (B007a), archive
	// and restore-archive (B007c) all run real provider/store effects.
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
	// state filters by §12.4 timestamp-derived lifecycle state. The empty value
	// (no filter) returns every session regardless of state — including archived
	// ones, so the governance console can drive restore (B007c). The dedicated
	// trash view (GET /admin/sessions/trash) remains the convenience surface for
	// the common case.
	stateFilter := q.Get("state")
	if stateFilter != "" && !validSessionState(stateFilter) {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			"state must be one of: active, trashed, archived")
		return
	}

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
		if !matchesSessionState(all[i], stateFilter) {
			continue
		}
		out = append(out, adminSessionToWebUI(all[i]))
	}

	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "count": len(out)})
}

// validSessionState reports whether s is a recognized §12.4 lifecycle-state
// filter value.
func validSessionState(s string) bool {
	switch s {
	case "active", "trashed", "archived":
		return true
	default:
		return false
	}
}

// matchesSessionState reports whether sess is in the requested §12.4
// timestamp-derived lifecycle state. An empty filter matches everything. State
// is derived from the lifecycle timestamps: archived (archived_at set) and
// trashed (trashed_at set) are checked first so a session in either terminal
// state is not also counted "active"; active = neither set.
func matchesSessionState(sess sessionmodel.AgentSession, state string) bool {
	switch state {
	case "":
		return true
	case "archived":
		return sess.ArchivedAt != nil
	case "trashed":
		return sess.TrashedAt != nil
	case "active":
		return sess.ArchivedAt == nil && sess.TrashedAt == nil
	default:
		return false
	}
}

// adminSessionToWebUI is the cross-tenant projection: the shared per-user
// projection plus the owner principal and the lifecycle timestamps the
// governance console needs to render and filter by state and drive restore. The
// per-user sessionToWebUI deliberately leaves these unset so the per-user surface
// is unchanged.
func adminSessionToWebUI(s sessionmodel.AgentSession) webUISession {
	out := sessionToWebUI(s, 0)
	out.CreatorPrincipal = s.CreatorPrincipal
	if s.TrashedAt != nil {
		out.TrashedAt = s.TrashedAt.Format(time.RFC3339)
	}
	if s.ArchivedAt != nil {
		out.ArchivedAt = s.ArchivedAt.Format(time.RFC3339)
	}
	return out
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
	writeJSON(w, http.StatusOK, adminSessionToWebUI(*sess))
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

// purgeRequest is the admin purge body. confirm=false (or absent) returns the
// §12.5 manifest-with-hard-stop and destroys NOTHING; confirm=true fires the
// irreversible expunge.
type purgeRequest struct {
	Confirm bool `json:"confirm"`
}

// handlePurge is the admin purge govern route (§12.5 manifest-with-hard-stop,
// admin-only). BOTH conditions (requireAdmin prefix + admin relationship via the
// admin seam) gate it. Without an explicit confirm it returns the manifest (count
// of messages destroyed and linked children severed) and leaves the session
// untouched — the hard stop. With confirm=true it runs PurgeSessionTx and writes
// the session.purge audit row in ONE transaction (same-tx effect↔audit coupling),
// then returns the manifest of what was destroyed.
func (h *adminSessionsHandler) handlePurge(w http.ResponseWriter, r *http.Request) {
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

	// BOTH-conditions (§12.8): requireAdmin (prefix) passed above; now the admin
	// RELATIONSHIP via the admin seam. A non-admin reaching here (only possible
	// under RBAC-off where the gate permits) is denied by the seam.
	decision, err := h.server.sessionAccessAdmin(r.Context(), principal, sessionID, sessionauthz.ActionPurge)
	if err != nil {
		writeInternalError(w, err, "authorize admin session purge")
		return
	}
	if !decision.Allowed {
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied: admin capability required")
		return
	}

	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "admin purge get session")
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	manifest, err := h.server.services.SessionModel.PurgeManifest(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "admin purge manifest")
		return
	}
	manifestJSON := map[string]any{
		"messages_destroyed":      manifest.MessageCount,
		"linked_children_severed": manifest.LinkedChildCount,
	}

	var req purgeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty/absent body ⇒ confirm=false
	}

	// Hard stop: without an explicit confirm, return the manifest and destroy
	// nothing. No audit row — nothing was governed yet, only previewed.
	if !req.Confirm {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":           "confirm_required",
			"requires_confirm": true,
			"manifest":         manifestJSON,
			"message":          "purge is irreversible: re-POST with {\"confirm\": true} to destroy this session and its transcript and sever its linked children",
		})
		return
	}

	// Confirmed: expunge + audit in ONE transaction. The audit row records the
	// manifest of what was destroyed/severed (the §12.5 incident link-sever is
	// recorded here as severed_children rather than a separate verb). Fail-closed:
	// an audit failure rolls back the expunge.
	ev := h.purgeEvent(principal, sessionID, manifest)
	if err := h.server.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return h.server.services.SessionModel.PurgeSessionTx(r.Context(), tx, sessionID)
	}); err != nil {
		writeInternalError(w, err, "admin purge session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "purged",
		"manifest": manifestJSON,
	})
}

// purgeEvent builds the same-transaction audit row for a confirmed purge. It
// records the manifest in the Details.After block so the destroyed/severed counts
// are durable alongside the action.
func (h *adminSessionsHandler) purgeEvent(principal rbac.Principal, sessionID string, m sessionmodel.PurgeManifest) audit.Event {
	blob, _ := json.Marshal(audit.Details{
		Target: "session:" + sessionID,
		After: map[string]string{
			"messages_destroyed":      strconv.Itoa(m.MessageCount),
			"linked_children_severed": strconv.Itoa(m.LinkedChildCount),
		},
	})
	return audit.Event{
		Principal: string(principal),
		Action:    audit.ActionSessionPurge,
		Decision:  audit.DecisionAllow,
		Reason:    "manifest_confirmed_purge",
		Kind:      audit.KindAdminAccess,
		Context:   string(blob),
	}
}

// handleArchive is the admin archive govern route (§12.6 provider seam, B007c).
// BOTH conditions gate it (requireAdmin prefix + admin relationship via the admin
// seam). It reads the session's LIVE transcript, writes the versioned
// self-contained artifact via the archive provider, then stamps
// archived_at/archived_by/archive_ref AND removes the hot transcript rows (the
// MOVE to cold storage) coupled to the session.archive audit row in ONE
// transaction (mutateWithAudit). Fail-closed: an audit failure rolls the state
// transition back and the archiver removes the orphaned artifact.
func (h *adminSessionsHandler) handleArchive(w http.ResponseWriter, r *http.Request) {
	principal, sess, ok := h.govern(w, r, sessionauthz.ActionArchive)
	if !ok {
		return
	}
	if h.server.services.SessionArchive == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "archive provider not configured")
		return
	}
	ev := h.archiveEvent(principal, audit.ActionSessionArchive, sess.ID, "admin_archive")
	commit := func(mutate func(*sql.Tx) error) error {
		return h.server.mutateWithAudit(r.Context(), ev, mutate)
	}
	ref, err := h.server.services.SessionArchive.Archive(r.Context(), *sess, string(principal), commit)
	if err != nil {
		if errors.Is(err, sessionmodel.ErrSessionAlreadyArchived) {
			writeError(w, http.StatusConflict, errorCodeInvalidRequest, "session is already archived")
			return
		}
		writeInternalError(w, err, "admin archive session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "archived",
		"archive_ref": ref,
	})
}

// handleUnarchive is the admin restore-archive govern route (§12.6, B007c). BOTH
// conditions gate it. It parses the provider-written artifact (refusing an
// unrecognized schema version — never silently accepting), then clears the
// archive columns AND rebuilds the hot transcript rows from the artifact coupled
// to the session.unarchive audit row in ONE transaction.
func (h *adminSessionsHandler) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	principal, sess, ok := h.govern(w, r, sessionauthz.ActionUnarchive)
	if !ok {
		return
	}
	if h.server.services.SessionArchive == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "archive provider not configured")
		return
	}
	if sess.ArchivedAt == nil {
		writeError(w, http.StatusConflict, errorCodeInvalidRequest, "session is not archived")
		return
	}
	ev := h.archiveEvent(principal, audit.ActionSessionUnarchive, sess.ID, "admin_restore_archive")
	commit := func(mutate func(*sql.Tx) error) error {
		return h.server.mutateWithAudit(r.Context(), ev, mutate)
	}
	err := h.server.services.SessionArchive.Restore(r.Context(), *sess, commit)
	switch {
	case errors.Is(err, sessionarchive.ErrUnsupportedArtifactVersion):
		// §12.6: refuse an unknown artifact version rather than mis-parse. 422 so
		// the caller learns the artifact was refused, never silently accepted.
		writeError(w, http.StatusUnprocessableEntity, errorCodeInvalidRequest,
			"archive artifact has an unsupported schema version; refusing to restore")
		return
	case errors.Is(err, sessionarchive.ErrNotArchived), errors.Is(err, sessionmodel.ErrSessionNotArchived):
		writeError(w, http.StatusConflict, errorCodeInvalidRequest, "session is not archived")
		return
	case err != nil:
		writeInternalError(w, err, "admin restore-archive session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "restored"})
}

// govern is the shared front half of the session-scoped admin-govern routes: it
// runs the requireAdmin prefix gate, the admin-relationship resolution through
// the admin seam (BOTH conditions, §12.8), and the target-session lookup. It
// returns the resolved admin principal and the session, or ok=false when it has
// already written the 400/403/404/503 response.
func (h *adminSessionsHandler) govern(w http.ResponseWriter, r *http.Request, action sessionauthz.Action) (rbac.Principal, *sessionmodel.AgentSession, bool) {
	principal, gated := h.server.requireAdmin(w, r)
	if gated {
		return "", nil, false
	}
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return "", nil, false
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return "", nil, false
	}
	decision, err := h.server.sessionAccessAdmin(r.Context(), principal, sessionID, action)
	if err != nil {
		writeInternalError(w, err, "authorize admin session govern")
		return "", nil, false
	}
	if !decision.Allowed {
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied: admin capability required")
		return "", nil, false
	}
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "admin govern get session")
		return "", nil, false
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return "", nil, false
	}
	return principal, sess, true
}

// archiveEvent builds the same-transaction audit row for an archive / unarchive
// govern transition. KindAdminAccess like the rest of the admin govern surface;
// the action verb (session.archive / session.unarchive) names which transition.
func (h *adminSessionsHandler) archiveEvent(principal rbac.Principal, action, sessionID, reason string) audit.Event {
	blob, _ := json.Marshal(audit.Details{Target: "session:" + sessionID})
	return audit.Event{
		Principal: string(principal),
		Action:    action,
		Decision:  audit.DecisionAllow,
		Reason:    reason,
		Kind:      audit.KindAdminAccess,
		Context:   string(blob),
	}
}

// handleListAllTrash is the cross-tenant all-trash list (§12.8). requireAdmin
// only (a team-wide read over trashed sessions); principal=nil lists every
// principal's trash.
func (h *adminSessionsHandler) handleListAllTrash(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.server.services.SessionModel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "count": 0})
		return
	}
	limit := 0 // no cap on the admin all-trash view
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	rows, err := h.server.services.SessionModel.ListTrashedSessions(r.Context(), nil, limit)
	if err != nil {
		writeInternalError(w, err, "admin all-trash list")
		return
	}
	out := make([]webUISession, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionToWebUI(row.AgentSession, row.MessageCount))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "count": len(out)})
}

// handleGetRetentionPolicy reads the admin retention policy (§12.5, migration
// 026). requireAdmin only (an ordinary admin read).
func (h *adminSessionsHandler) handleGetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}
	p, err := h.server.services.SessionModel.GetRetentionPolicy(r.Context())
	if err != nil {
		writeInternalError(w, err, "get retention policy")
		return
	}
	writeJSON(w, http.StatusOK, retentionPolicyToJSON(p))
}

// retentionPolicyRequest is the configure_retention PUT body (§12.5 knobs).
// InactivityDays nil / "off" ⇒ OFF (default). The legacy B006 body used
// "inactivity_window" as a string ("off"); both are accepted.
type retentionPolicyRequest struct {
	InactivityDays   *int    `json:"inactivity_days"`
	InactivityWindow *string `json:"inactivity_window"`
	TrashGraceDays   *int    `json:"trash_grace_days"`
	TerminalAction   *string `json:"terminal_action"`
}

// handlePutRetentionPolicy is the configure_retention govern route (§12.5
// inactivity-window / trash-grace / terminal-action). BOTH conditions gate it
// (requireAdmin + admin relationship). It writes the policy and its
// session.configure_retention audit row in ONE transaction (same-tx coupling).
func (h *adminSessionsHandler) handlePutRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	principal, gated := h.server.requireAdmin(w, r)
	if gated {
		return
	}
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
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

	// Start from the current policy so a partial PUT updates only the named knobs.
	current, err := h.server.services.SessionModel.GetRetentionPolicy(r.Context())
	if err != nil {
		writeInternalError(w, err, "load retention policy")
		return
	}
	var req retentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	next, verr := mergeRetentionPolicy(*current, req)
	if verr != "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, verr)
		return
	}

	ev := audit.Event{
		Principal: string(principal),
		Action:    audit.ActionSessionConfigureRetention,
		Decision:  audit.DecisionAllow,
		Reason:    "configure_retention",
		Kind:      audit.KindAdminAccess,
		Context:   retentionAuditContext(next),
	}
	if err := h.server.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return h.server.services.SessionModel.SetRetentionPolicyTx(r.Context(), tx, next, string(principal), time.Now().UTC())
	}); err != nil {
		writeInternalError(w, err, "configure retention policy")
		return
	}

	writeJSON(w, http.StatusOK, retentionPolicyToJSON(&next))
}

// mergeRetentionPolicy applies the PUT body onto the current policy, validating
// the knobs. It returns a non-empty string describing the first validation
// failure. "off" / null inactivity ⇒ OFF (nil).
func mergeRetentionPolicy(cur sessionmodel.RetentionPolicy, req retentionPolicyRequest) (sessionmodel.RetentionPolicy, string) {
	next := cur
	switch {
	case req.InactivityDays != nil:
		if *req.InactivityDays < 0 {
			return next, "inactivity_days must be >= 0 (or omit / \"off\" for no auto-expiry)"
		}
		if *req.InactivityDays == 0 {
			next.InactivityDays = nil // 0 ⇒ off
		} else {
			d := *req.InactivityDays
			next.InactivityDays = &d
		}
	case req.InactivityWindow != nil:
		if strings.EqualFold(strings.TrimSpace(*req.InactivityWindow), "off") || strings.TrimSpace(*req.InactivityWindow) == "" {
			next.InactivityDays = nil
		} else {
			return next, "inactivity_window accepts only \"off\"; use inactivity_days for a numeric window"
		}
	}
	if req.TrashGraceDays != nil {
		if *req.TrashGraceDays < 0 {
			return next, "trash_grace_days must be >= 0"
		}
		next.TrashGraceDays = *req.TrashGraceDays
	}
	if req.TerminalAction != nil {
		ta := sessionmodel.TerminalAction(strings.TrimSpace(*req.TerminalAction))
		if ta != sessionmodel.TerminalActionTrashThenPurge && ta != sessionmodel.TerminalActionArchive {
			return next, "terminal_action must be \"trash_then_purge\" or \"archive\""
		}
		next.TerminalAction = ta
	}
	return next, ""
}

// retentionPolicyToJSON projects a policy to the API shape. inactivity_days is
// null when OFF (the §12.5 default), with an inactivity_window:"off" convenience
// mirror so a client can render either form.
func retentionPolicyToJSON(p *sessionmodel.RetentionPolicy) map[string]any {
	out := map[string]any{
		"trash_grace_days": p.TrashGraceDays,
		"terminal_action":  string(p.TerminalAction),
	}
	if p.InactivityDays != nil {
		out["inactivity_days"] = *p.InactivityDays
		out["inactivity_window"] = strconv.Itoa(*p.InactivityDays) + "d"
	} else {
		out["inactivity_days"] = nil
		out["inactivity_window"] = "off"
	}
	if p.UpdatedAt != nil {
		out["updated_at"] = p.UpdatedAt.Format(time.RFC3339)
	}
	if p.UpdatedBy != nil {
		out["updated_by"] = *p.UpdatedBy
	}
	return out
}

// retentionAuditContext renders the policy knobs into the audit Details.After
// block so the configured values are durable on the audit row.
func retentionAuditContext(p sessionmodel.RetentionPolicy) string {
	inactivity := "off"
	if p.InactivityDays != nil {
		inactivity = strconv.Itoa(*p.InactivityDays)
	}
	blob, _ := json.Marshal(audit.Details{
		Target: "session_retention_policy",
		After: map[string]string{
			"inactivity_days":  inactivity,
			"trash_grace_days": strconv.Itoa(p.TrashGraceDays),
			"terminal_action":  string(p.TerminalAction),
		},
	})
	return string(blob)
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
