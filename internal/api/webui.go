package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionauthz"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/uid"
)

// webUINode is the web UI representation of a graph node.
type webUINode struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Namespace string         `json:"namespace,omitempty"`
	Cluster   string         `json:"cluster,omitempty"`
	Metadata  map[string]any `json:"metadata"`
	Labels    map[string]any `json:"labels,omitempty"`
	Status    string         `json:"status,omitempty"`
}

// webUIEdge is the web UI representation of a graph edge.
type webUIEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// webUIHandler handles Web UI-specific API requests.
type webUIHandler struct {
	server *Server
}

func nodeToWebUI(n graph.Node) webUINode {
	meta := n.Metadata
	if meta == nil {
		meta = map[string]any{}
	}

	getString := func(key string) string {
		if v, ok := meta[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	name := getString("name")
	if name == "" {
		name = n.ID
	}

	var labels map[string]any
	if v, ok := meta["labels"]; ok {
		if m, ok := v.(map[string]any); ok {
			labels = m
		}
	}

	return webUINode{
		ID:        n.ID,
		Kind:      n.Type,
		Name:      name,
		Namespace: getString("namespace"),
		Cluster:   getString("cluster"),
		Metadata:  meta,
		Labels:    labels,
		Status:    getString("status"),
	}
}

func edgeToWebUI(e graph.Edge) webUIEdge {
	return webUIEdge{
		ID:     fmt.Sprintf("%s-%s-%s", e.From, e.Relation, e.To),
		Source: e.From,
		Target: e.To,
		Type:   e.Relation,
	}
}

// handleGetFullGraph returns all nodes and edges in web UI format.
func (h *webUIHandler) handleGetFullGraph(w http.ResponseWriter, r *http.Request) {
	if !h.server.accessor.GraphAvailable() {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}, "edges": []any{}})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	subgraph, err := h.server.accessor.GraphListAll(r.Context(), principal)
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		writeInternalError(w, err, "list all graph nodes")
		return
	}

	nodes := make([]webUINode, len(subgraph.Nodes))
	for i, n := range subgraph.Nodes {
		nodes[i] = nodeToWebUI(n)
	}

	edges := make([]webUIEdge, len(subgraph.Edges))
	for i, e := range subgraph.Edges {
		edges[i] = edgeToWebUI(e)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		"edges": edges,
	})
}

// handleGetNode returns a single node by ID in web UI format.
func (h *webUIHandler) handleGetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing node id")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	n, err := h.server.accessor.GraphGetNode(r.Context(), principal, nodeID)
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		writeInternalError(w, err, "get graph node")
		return
	}
	if n == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "node not found")
		return
	}

	writeJSON(w, http.StatusOK, nodeToWebUI(*n))
}

// handleGetRelatedNodes returns related nodes for a given node ID.
func (h *webUIHandler) handleGetRelatedNodes(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing node id")
		return
	}

	depth := 1
	if d := r.URL.Query().Get("depth"); d != "" {
		parsed, err := strconv.Atoi(d)
		if err == nil && parsed > 0 {
			depth = parsed
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	subgraph, err := h.server.accessor.GraphRelated(r.Context(), principal, nodeID, depth)
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		writeInternalError(w, err, "graph related")
		return
	}

	nodes := make([]webUINode, len(subgraph.Nodes))
	for i, n := range subgraph.Nodes {
		nodes[i] = nodeToWebUI(n)
	}
	edges := make([]webUIEdge, len(subgraph.Edges))
	for i, e := range subgraph.Edges {
		edges[i] = edgeToWebUI(e)
	}

	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "edges": edges})
}

// webUISession is the Web UI representation of a chat session. It is the legacy
// (migration-001) JSON shape the chat UI already consumes, projected from an
// agent_sessions row so the model swap (DESIGN-CHAT-SESSIONS.md §11 Phase 1) is
// invisible to the frontend. title/visibility are additive — the frontend
// ignores them until the Phase 2/3 UI work.
type webUISession struct {
	ID           string `json:"id"`
	StartedAt    string `json:"started_at"`
	LastActivity string `json:"last_activity_at,omitempty"`
	Summary      string `json:"summary,omitempty"`
	MessageCount int    `json:"message_count"`
	Title        string `json:"title,omitempty"`
	// ReadOnly is true when the caller is not the owner (team-wide read model,
	// §12: any authenticated principal may read any session, but only the owner
	// may write). The owner-scoped list/create/rename paths leave it false.
	//
	// NOT omitempty on purpose: read_only must ALWAYS be present so the client can
	// gate owner-only controls (the visibility toggle, rename) on a POSITIVE owner
	// signal (read_only === false) and fail CLOSED when the field is absent. With
	// omitempty, an owner's response (read_only=false) omitted the field, which a
	// "!read_only" client check read as "owner" — but so did a stale/partial cache
	// entry with no field at all, letting a non-owner's view briefly present the
	// toggle and fire a PATCH that the backend then 404s ("session not found").
	ReadOnly bool `json:"read_only"`
	// LinkedIncidentID is the id of the active incident this session has been
	// attached to (Phase 4 incident linkage), or empty when unlinked. The
	// browse list and chat header use its presence to show an incident badge.
	LinkedIncidentID string `json:"linked_incident_id,omitempty"`
	// SharedBy is the owning principal of a public session surfaced in another
	// user's "shared with you" list (DESIGN-CHAT-SESSIONS.md §10 sharing
	// extension). It is the one place the owner identity is intentionally
	// projected — the owner-scoped list, create/rename, and the per-id GET all
	// leave it empty. Its presence lets the UI label a row "shared by <owner>".
	SharedBy string `json:"shared_by,omitempty"`
	// CreatorPrincipal is the owning principal, surfaced on the ADMIN cross-tenant
	// list so the governance console can show ownership and filter by principal.
	// omitempty keeps it out of the per-user projections that don't set it.
	CreatorPrincipal string `json:"creator_principal,omitempty"`
	// TrashedAt / ArchivedAt are the §12.4 lifecycle timestamps, surfaced on the
	// admin cross-tenant list so the governance console can render and filter by
	// lifecycle state (active / trashed / archived) and drive restore. Both empty
	// (omitted) on an active session, and on the per-user projections that leave
	// them unset.
	TrashedAt  string `json:"trashed_at,omitempty"`
	ArchivedAt string `json:"archived_at,omitempty"`
	// PurgeAfter is the §12.5 trash-grace deadline (now + trash-grace at
	// soft-delete time); the UI trash view subtracts it from the wall clock to show
	// the remaining time before automatic purge. Set only on a trashed row (the
	// underlying column is null otherwise), so omitempty keeps it out of every
	// active-session projection — a read-only projection of an existing column, no
	// lifecycle behavior attached.
	PurgeAfter string `json:"purge_after,omitempty"`
}

// webUIMessage is the Web UI representation of a chat message — the legacy flat
// shape. id carries the per-session seq (a number, matching the frontend's
// numeric id field). tool_args is intentionally omitted: Phase 1 only persists
// user/assistant turns, and the frontend expects a structured object there.
type webUIMessage struct {
	ID        int    `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolName  string `json:"tool_name,omitempty"`
	CreatedAt string `json:"created_at"`
}

func sessionToWebUI(s sessionmodel.AgentSession, messageCount int) webUISession {
	out := webUISession{
		ID:           s.ID,
		StartedAt:    s.CreatedAt.Format(time.RFC3339),
		LastActivity: s.LastActivityAt.Format(time.RFC3339),
		MessageCount: messageCount,
	}
	if s.LinkedIncidentID != nil {
		out.LinkedIncidentID = *s.LinkedIncidentID
	}
	// purge_after is surfaced whenever it is set (a trashed row under the
	// trash-then-purge policy) so both the per-user and admin trash views can
	// render the remaining time before purge. An active row leaves it nil/omitted.
	if s.PurgeAfter != nil {
		out.PurgeAfter = s.PurgeAfter.Format(time.RFC3339)
	}
	if s.Title != nil {
		out.Title = *s.Title
		// summary is the field the existing dashboard RecentSessions labels by;
		// mirror title into it so a Phase 2 auto-title surfaces with no frontend
		// change.
		out.Summary = *s.Title
	}
	return out
}

func messageToWebUI(m sessionmodel.ChatMessage) webUIMessage {
	out := webUIMessage{
		ID:        m.Seq,
		SessionID: m.SessionID,
		Role:      m.Role,
		Content:   m.Content,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
	if m.ToolName != nil {
		out.ToolName = *m.ToolName
	}
	return out
}

// handleListSessions returns the team-wide session list with an optional `mine`
// filter (DESIGN-CHAT-SESSIONS.md §12.8, team-wide read amendment 2026-06-21).
// This collapses the as-built owner-scoped vs "shared with you" two-route split
// into ONE route:
//   - default (no filter): the TEAM-WIDE list — every session, since any
//     authenticated principal may read any session. Each row is stamped
//     read_only per ownership (a non-owner is read-only on a session it does not
//     own) and carries shared_by (the owner) on rows the caller does not own, so
//     the UI can label and gate them without a second request.
//   - ?mine=true: the caller-scoped list (the session creator's own sessions),
//     all read_only=false.
//
// There is NO visibility concept (B002 removed it); read is the default for any
// authenticated principal, not a per-session grant.
func (h *webUIHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "count": 0})
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	principal := string(rbac.PrincipalFromContext(r.Context()))
	mine := r.URL.Query().Get("mine") == "true"

	var (
		rows []sessionmodel.ChatSessionRow
		err  error
	)
	if mine {
		rows, err = h.server.services.SessionModel.ListSessionsByCreator(r.Context(), principal, limit)
	} else {
		rows, err = h.server.services.SessionModel.ListRecentSessions(r.Context(), limit)
	}
	if err != nil {
		writeInternalError(w, err, "list sessions")
		return
	}

	sessions := make([]webUISession, 0, len(rows))
	for _, row := range rows {
		s := sessionToWebUI(row.AgentSession, row.MessageCount)
		// Team-wide read: rows the caller does not own are read-only viewers. The
		// `mine` list is owner-scoped, so every row there is the caller's own.
		if row.CreatorPrincipal != principal {
			s.ReadOnly = true
			s.SharedBy = row.CreatorPrincipal
		}
		sessions = append(sessions, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleCreateSession creates a new chat session owned by the caller. The owner
// (creator_principal) is recorded from the request context — the legacy handler
// recorded no owner at all, which is what made cross-user reads possible.
func (h *webUIHandler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}

	principal := string(rbac.PrincipalFromContext(r.Context()))
	now := time.Now().UTC()
	created, err := h.server.services.SessionModel.CreateSession(r.Context(), sessionmodel.AgentSession{
		ID:               uid.New(),
		Type:             sessionmodel.SessionTypeDefault,
		CreatedAt:        now,
		LastActivityAt:   now,
		CreatorPrincipal: principal,
	})
	if err != nil {
		writeInternalError(w, err, "create session")
		return
	}

	writeJSON(w, http.StatusCreated, sessionToWebUI(*created, 0))
}

// handleGetSession returns a single session's metadata. Any authenticated user
// may read any session that exists (the org-wide read model); only the creator
// may write to it. read_only=false marks the caller as the owner, read_only=true
// a non-owner reader — the client gates write affordances (composer, rename,
// share-link) on that. A missing session returns 404.
func (h *webUIHandler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	principal := string(rbac.PrincipalFromContext(r.Context()))
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	messages, err := h.server.services.SessionModel.ListChatMessages(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session messages")
		return
	}

	out := sessionToWebUI(*sess, len(messages))
	out.ReadOnly = sess.CreatorPrincipal != principal
	writeJSON(w, http.StatusOK, out)
}

// handleGetSessionMessages returns a session's messages. Any authenticated user
// may read any session that exists (org-wide read model); writes stay owner-only
// on the task path. A missing session returns 404.
func (h *webUIHandler) handleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "count": 0})
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	messages, err := h.server.services.SessionModel.ListChatMessages(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session messages")
		return
	}

	out := make([]webUIMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, messageToWebUI(m))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": out,
		"count":    len(out),
	})
}

// updateSessionRequest is the PATCH /sessions/{id} body. Only the title is
// mutable (rename). Sharing has no toggle in the org-wide read model — every
// session is readable by any authenticated user; only the owner may write.
type updateSessionRequest struct {
	Title *string `json:"title"`
}

// handleUpdateSession renames a session the caller owns (PATCH /sessions/{id}).
// Owner-checked: a non-owner (or a missing session) gets 404 — writes are
// owner-only even though reads are open. The diagnostic distinguishes a missing
// session from an ownership mismatch so a stray rename is self-explaining.
func (h *webUIHandler) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	var req updateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	if req.Title == nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "title is required")
		return
	}
	title := strings.TrimSpace(*req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "title must not be empty")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	// Authorize the rename (owner-mutate 'write') through the single session
	// seam (§12.7) instead of an inline creator comparison. A denied decision
	// keeps the 404-on-non-owner posture (a non-owner's session is
	// indistinguishable from a missing one); the diagnostic still separates a
	// missing session from an ownership mismatch.
	decision, err := h.server.sessionAccess(r.Context(), principal, sessionID, sessionauthz.ActionWrite)
	if err != nil {
		writeInternalError(w, err, "authorize session write")
		return
	}
	if sess == nil || !decision.Allowed {
		reason := "session_missing"
		if sess != nil {
			reason = "owner_mismatch"
		}
		slog.Warn("update session: 404",
			"reason", reason,
			"session_id", sessionID,
			"request_principal", string(principal),
			"relationship", string(decision.Relationship))
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	if err := h.server.services.SessionModel.UpdateSessionTitle(r.Context(), sessionID, title); err != nil {
		writeInternalError(w, err, "update session title")
		return
	}
	sess.Title = &title

	writeJSON(w, http.StatusOK, sessionToWebUI(*sess, 0))
}

// handleDeleteSession soft-deletes a session the caller owns to trash (DELETE
// /sessions/{id}, §12.5 macOS-trash). This is the audited 'soft_delete' (trash)
// transition (B007a): it sets trashed_at/trashed_by and a purge_after deadline
// derived from the retention policy's trash-grace, and writes the
// session.trash audit row in the SAME transaction as the column write (§12.5
// every transition is audited; same-tx effect↔audit coupling). The session is
// NOT physically removed — it is recoverable via restore until the sweeper or an
// admin purges it. Owner-checked through the seam, 404-on-non-owner.
func (h *webUIHandler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	// Authorize the soft-delete (owner-mutate 'soft_delete') through the single
	// session seam (§12.7). 404-on-non-owner posture preserved (a non-owner's
	// session is indistinguishable from a missing one).
	decision, err := h.server.sessionAccess(r.Context(), principal, sessionID, sessionauthz.ActionSoftDelete)
	if err != nil {
		writeInternalError(w, err, "authorize session soft-delete")
		return
	}
	if sess == nil || !decision.Allowed {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	// purge_after = now + trash-grace from the active retention policy (§12.5):
	// a manual soft-delete enters trash with the same grace deadline the sweeper
	// would apply. A missing policy degrades to no deadline rather than failing
	// the delete.
	purgeAfter := h.server.trashGraceDeadline(r.Context())

	ev := sessionLifecycleEvent(principal, audit.ActionSessionTrash, sessionID)
	if err := h.server.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return h.server.services.SessionModel.TrashSessionTx(r.Context(), tx, sessionID, string(principal), purgeAfter)
	}); err != nil {
		writeInternalError(w, err, "soft-delete session")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRestoreSession restores a trashed session the caller owns back to active
// (POST /sessions/{id}/restore, §12.5). Audited 'restore' transition: clears the
// trash columns and writes the session.restore audit row in the same
// transaction. Owner-checked through the seam (action=restore), 404-on-non-owner.
// Returns 409 if the session is not currently trashed.
func (h *webUIHandler) handleRestoreSession(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	decision, err := h.server.sessionAccess(r.Context(), principal, sessionID, sessionauthz.ActionRestore)
	if err != nil {
		writeInternalError(w, err, "authorize session restore")
		return
	}
	if sess == nil || !decision.Allowed {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	ev := sessionLifecycleEvent(principal, audit.ActionSessionRestore, sessionID)
	if err := h.server.mutateWithAudit(r.Context(), ev, func(tx *sql.Tx) error {
		return h.server.services.SessionModel.RestoreSessionTx(r.Context(), tx, sessionID)
	}); err != nil {
		if errors.Is(err, sessionmodel.ErrSessionNotTrashed) {
			writeError(w, http.StatusConflict, errorCodeConflict, "session is not in trash")
			return
		}
		writeInternalError(w, err, "restore session")
		return
	}

	sess.TrashedAt, sess.TrashedBy, sess.PurgeAfter = nil, nil, nil
	writeJSON(w, http.StatusOK, sessionToWebUI(*sess, 0))
}

// handleListOwnTrash lists the caller's own trashed sessions (GET
// /sessions/trash, §12.8). Owner-scoped by construction (filters on the resolved
// principal), so no per-row seam check is needed — a caller only ever sees their
// own trash, with the remaining time before purge carried per row (purge_after).
func (h *webUIHandler) handleListOwnTrash(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "count": 0})
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	principal := string(rbac.PrincipalFromContext(r.Context()))
	rows, err := h.server.services.SessionModel.ListTrashedSessions(r.Context(), &principal, limit)
	if err != nil {
		writeInternalError(w, err, "list own trash")
		return
	}

	out := make([]webUISession, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionToWebUI(row.AgentSession, row.MessageCount))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "count": len(out)})
}

// trashGraceDeadline computes the §12.5 purge_after deadline (now + the retention
// policy's trash-grace window) for a manual soft-delete. Returns nil when no
// policy is configured (or the store is unavailable) so a soft-delete still
// succeeds — it just enters trash with no auto-purge deadline.
func (s *Server) trashGraceDeadline(ctx context.Context) *time.Time {
	if s.services.SessionModel == nil {
		return nil
	}
	p, err := s.services.SessionModel.GetRetentionPolicy(ctx)
	if err != nil || p == nil || p.TrashGraceDays <= 0 {
		return nil
	}
	deadline := time.Now().UTC().Add(time.Duration(p.TrashGraceDays) * 24 * time.Hour)
	return &deadline
}

// sessionLifecycleEvent builds the §12.5 audit row for a per-user owner lifecycle
// transition (trash / restore). It carries KindSessionLifecycle (owner action,
// not admin surface) and names the actor and target session.
func sessionLifecycleEvent(principal rbac.Principal, action, sessionID string) audit.Event {
	blob, _ := json.Marshal(audit.Details{Target: "session:" + sessionID})
	return audit.Event{
		Principal: string(principal),
		Action:    action,
		Decision:  audit.DecisionAllow,
		Reason:    "owner_lifecycle_transition",
		Kind:      audit.KindSessionLifecycle,
		Context:   string(blob),
	}
}

// handleLinkIncident attaches the caller's chat session to the currently-active
// incident (POST /sessions/{id}/link-incident, DESIGN-CHAT-SESSIONS.md §12.3).
// Under the two-type model participation is the linked_incident_id pointer
// ALONE — there is no type flip (the 'investigation' type was removed), so the
// session stays a plain 'default' conversation; captaincy is out of scope.
// Owner-checked
// with the same 404-on-miss posture as the other mutators (another user's
// session is indistinguishable from a missing one). Returns 409 when there is
// no active incident to link to. Linking is idempotent: re-linking to the same
// active incident returns 200 with the unchanged linkage.
func (h *webUIHandler) handleLinkIncident(w http.ResponseWriter, r *http.Request) {
	if h.server.services.SessionModel == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "session store not available")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	// Authorize linking (owner-mutate 'write' — §12.7 "write (rename, link,
	// send-message)") through the single session seam. 404-on-non-owner posture
	// preserved.
	decision, err := h.server.sessionAccess(r.Context(), principal, sessionID, sessionauthz.ActionWrite)
	if err != nil {
		writeInternalError(w, err, "authorize session link")
		return
	}
	if sess == nil || !decision.Allowed {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}
	// An incident session itself cannot carry linked_incident_id (migration-009
	// CHECK), so refuse rather than emit a write the DB would reject.
	if sess.Type == sessionmodel.SessionTypeIncident {
		writeError(w, http.StatusConflict, errorCodeConflict, "an incident session cannot be linked to an incident")
		return
	}

	incident, err := h.server.services.SessionModel.ActiveIncidentSession(r.Context())
	if err != nil {
		writeInternalError(w, err, "find active incident")
		return
	}
	if incident == nil {
		writeError(w, http.StatusConflict, errorCodeConflict, "no active incident to link to")
		return
	}
	if incident.ID == sessionID {
		writeError(w, http.StatusConflict, errorCodeConflict, "a session cannot be linked to itself")
		return
	}

	if err := h.server.services.SessionModel.LinkSessionToIncident(r.Context(), sessionID, incident.ID); err != nil {
		writeInternalError(w, err, "link session to incident")
		return
	}
	// Two-type model (§12.3): linkage is the linked_incident_id pointer alone —
	// no type flip (the 'investigation' type was removed). The session stays a
	// plain 'default' conversation that participates via the pointer.
	sess.LinkedIncidentID = &incident.ID

	writeJSON(w, http.StatusOK, sessionToWebUI(*sess, 0))
}

// handleGetAlerts returns an aggregated list of active alerts (stub).
func (h *webUIHandler) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	// TODO: aggregate from Alertmanager/Grafana components
	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": []any{},
		"count":  0,
	})
}

// handleTestComponent tests whether a source connection is healthy by building a
// fresh adapter and actually connecting to the backend. On success the live
// adapter is (re)registered — self-healing an "adapter not found" state left by
// a failed startup connect — and the source's error status is cleared; on
// failure the live error is persisted so the components list reflects reality.
//
// HTTP 200 is returned for both connect outcomes (the JSON "ok" field carries
// the result) so that request-level failures (400/404/503) stay distinct from a
// reachable-but-unhealthy backend.
func (h *webUIHandler) handleTestComponent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id")
		return
	}

	if h.server.services.Store == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "store not available")
		return
	}

	ctx := r.Context()
	src, err := h.server.services.Store.Components.Get(ctx, id)
	if err != nil {
		writeInternalError(w, err, "get source for test")
		return
	}
	if src == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found")
		return
	}

	adapter := newAdapterForType(src.Type)
	if adapter == nil {
		// Config-only source type: nothing to connect to.
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": fmt.Sprintf("source %q is configured (type %q has no connection to test)", src.ID, src.Type),
		})
		return
	}

	if err := adapter.Connect(ctx, *src); err != nil {
		// Persist the live failure so the list status agrees with the test.
		if updateErr := h.server.services.Store.Components.UpdateSyncStatus(ctx, src.ID, time.Now(), err.Error()); updateErr != nil {
			slog.Warn("failed to persist source test status", "component_id", src.ID, "error", updateErr)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      false,
			"message": fmt.Sprintf("connection failed: %v", err),
		})
		return
	}

	// Connected: register the live adapter so the refresh loop and tools can use
	// it, and clear the stored error so the list status recovers.
	if h.server.services.Adapters != nil {
		h.server.services.Adapters.Register(src.ID, adapter)
	}
	if updateErr := h.server.services.Store.Components.UpdateSyncStatus(ctx, src.ID, time.Now(), ""); updateErr != nil {
		slog.Warn("failed to persist source test status", "component_id", src.ID, "error", updateErr)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "connection successful",
	})
}

func (s *Server) registerWebUIRoutes(mux *http.ServeMux, prefix string) {
	h := &webUIHandler{server: s}

	// Graph - web UI format
	mux.HandleFunc(fmt.Sprintf("GET %s/graph", prefix), h.handleGetFullGraph)
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/node/{id}", prefix), h.handleGetNode)
	mux.HandleFunc(fmt.Sprintf("GET %s/graph/node/{id}/related", prefix), h.handleGetRelatedNodes)

	// Sessions — one per-user surface (§12.8). The team-wide list with a `mine`
	// filter replaces the as-built owner-scoped + "shared with you" split; the
	// separate GET /sessions/shared route is removed (no visibility concept).
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions", prefix), h.handleListSessions)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions", prefix), h.handleCreateSession)
	// List own trash (§12.8). The literal "trash" segment takes precedence over
	// the {id} wildcard in Go's ServeMux, so it coexists with GET /sessions/{id}.
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/trash", prefix), h.handleListOwnTrash)
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}", prefix), h.handleGetSession)
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}/messages", prefix), h.handleGetSessionMessages)
	mux.HandleFunc(fmt.Sprintf("PATCH %s/sessions/{id}", prefix), h.handleUpdateSession)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/sessions/{id}", prefix), h.handleDeleteSession)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/restore", prefix), h.handleRestoreSession)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/link-incident", prefix), h.handleLinkIncident)

	// Alerts aggregation
	mux.HandleFunc(fmt.Sprintf("GET %s/alerts", prefix), h.handleGetAlerts)

	// Source test
	mux.HandleFunc(fmt.Sprintf("POST %s/components/{id}/test", prefix), h.handleTestComponent)
}
