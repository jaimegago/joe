package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
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
	Visibility   string `json:"visibility,omitempty"`
	// ReadOnly is true when the caller is not the owner and is viewing the
	// session only because it is public (DESIGN-CHAT-SESSIONS.md §10 access
	// matrix: non-owner public = read-only). Set only by handleGetSession; the
	// owner-scoped list/create/rename paths leave it false (omitted).
	ReadOnly bool `json:"read_only,omitempty"`
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
		Visibility:   s.Visibility,
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

// handleListSessions returns the caller's own sessions (owner-scoped). This is
// the §11 Phase 1 isolation fix: the legacy handler called ListRecent with no
// principal filter, so any logged-in user could enumerate every user's chat
// history. Now it filters by the caller's principal.
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
	rows, err := h.server.services.SessionModel.ListSessionsByCreator(r.Context(), principal, limit)
	if err != nil {
		writeInternalError(w, err, "list sessions")
		return
	}

	sessions := make([]webUISession, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, sessionToWebUI(row.AgentSession, row.MessageCount))
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
		Type:             sessionmodel.SessionTypeOther,
		CreatedAt:        now,
		LastActivityAt:   now,
		CreatorPrincipal: principal,
		Visibility:       sessionmodel.VisibilityPrivate,
	})
	if err != nil {
		writeInternalError(w, err, "create session")
		return
	}

	writeJSON(w, http.StatusCreated, sessionToWebUI(*created, 0))
}

// handleGetSession returns a single session's metadata for the caller. The
// owner sees it (read_only=false); a non-owner sees it only when it is public
// (read_only=true, DESIGN-CHAT-SESSIONS.md §10 access matrix); a private session
// owned by someone else — or a missing one — returns 404 (existence not
// disclosed). creator_principal is never projected, so a public viewer does not
// learn the owner's identity.
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
	isOwner := sess != nil && sess.CreatorPrincipal == principal
	if sess == nil || (!isOwner && sess.Visibility != sessionmodel.VisibilityPublic) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	messages, err := h.server.services.SessionModel.ListChatMessages(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session messages")
		return
	}

	out := sessionToWebUI(*sess, len(messages))
	out.ReadOnly = !isOwner
	writeJSON(w, http.StatusOK, out)
}

// handleGetSessionMessages returns messages for a session the caller may read:
// the owner always, or any authenticated principal when the session is public
// (read-only, DESIGN-CHAT-SESSIONS.md §10 access matrix). A private session
// owned by another principal — or one that does not exist — returns 404 (not
// 403): the existence of another user's private session is not disclosed.
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

	principal := string(rbac.PrincipalFromContext(r.Context()))
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil || (sess.CreatorPrincipal != principal && sess.Visibility != sessionmodel.VisibilityPublic) {
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

// updateSessionRequest is the PATCH /sessions/{id} body. Both fields are
// optional pointers so a caller can send either (title rename — Phase 2; or
// visibility toggle — Phase 3) without clobbering the other, and so "field
// omitted" is distinguishable from "field set to empty string".
type updateSessionRequest struct {
	Title      *string `json:"title"`
	Visibility *string `json:"visibility"`
}

// handleUpdateSession mutates a session the caller owns (PATCH /sessions/{id}):
// rename (Phase 2) and/or visibility toggle (Phase 3). Owner-checked with the
// same 404-on-miss posture as the messages endpoint — another user's session
// must not be distinguishable from a missing one (DESIGN-CHAT-SESSIONS.md §10
// access matrix). Inputs are validated before any write so an invalid request
// never half-applies.
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
	if req.Title == nil && req.Visibility == nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "title or visibility is required")
		return
	}

	// Validate everything up front so a bad value never leaves a partial write.
	var title string
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "title must not be empty")
			return
		}
	}
	if req.Visibility != nil {
		if *req.Visibility != sessionmodel.VisibilityPrivate && *req.Visibility != sessionmodel.VisibilityPublic {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "visibility must be 'private' or 'public'")
			return
		}
	}

	principal := string(rbac.PrincipalFromContext(r.Context()))
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil || sess.CreatorPrincipal != principal {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	if req.Title != nil {
		if err := h.server.services.SessionModel.UpdateSessionTitle(r.Context(), sessionID, title); err != nil {
			writeInternalError(w, err, "update session title")
			return
		}
		sess.Title = &title
	}
	if req.Visibility != nil {
		if err := h.server.services.SessionModel.UpdateSessionVisibility(r.Context(), sessionID, *req.Visibility); err != nil {
			writeInternalError(w, err, "update session visibility")
			return
		}
		sess.Visibility = *req.Visibility
	}

	writeJSON(w, http.StatusOK, sessionToWebUI(*sess, 0))
}

// handleDeleteSession deletes a session the caller owns (DELETE /sessions/{id}).
// Owner-checked, 404-on-miss. The chat_messages FK is ON DELETE CASCADE, so the
// session's messages are expunged with it (migration 022).
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

	principal := string(rbac.PrincipalFromContext(r.Context()))
	sess, err := h.server.services.SessionModel.GetSession(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session")
		return
	}
	if sess == nil || sess.CreatorPrincipal != principal {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	if err := h.server.services.SessionModel.DeleteSession(r.Context(), sessionID); err != nil {
		writeInternalError(w, err, "delete session")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetAlerts returns an aggregated list of active alerts (stub).
func (h *webUIHandler) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	// TODO: aggregate from Alertmanager/Grafana sources
	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": []any{},
		"count":  0,
	})
}

// handleTestSource tests whether a source connection is healthy by building a
// fresh adapter and actually connecting to the backend. On success the live
// adapter is (re)registered — self-healing an "adapter not found" state left by
// a failed startup connect — and the source's error status is cleared; on
// failure the live error is persisted so the sources list reflects reality.
//
// HTTP 200 is returned for both connect outcomes (the JSON "ok" field carries
// the result) so that request-level failures (400/404/503) stay distinct from a
// reachable-but-unhealthy backend.
func (h *webUIHandler) handleTestSource(w http.ResponseWriter, r *http.Request) {
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
	src, err := h.server.services.Store.Sources.Get(ctx, id)
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
		if updateErr := h.server.services.Store.Sources.UpdateSyncStatus(ctx, src.ID, time.Now(), err.Error()); updateErr != nil {
			slog.Warn("failed to persist source test status", "source_id", src.ID, "error", updateErr)
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
	if updateErr := h.server.services.Store.Sources.UpdateSyncStatus(ctx, src.ID, time.Now(), ""); updateErr != nil {
		slog.Warn("failed to persist source test status", "source_id", src.ID, "error", updateErr)
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

	// Sessions
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions", prefix), h.handleListSessions)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions", prefix), h.handleCreateSession)
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}", prefix), h.handleGetSession)
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}/messages", prefix), h.handleGetSessionMessages)
	mux.HandleFunc(fmt.Sprintf("PATCH %s/sessions/{id}", prefix), h.handleUpdateSession)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/sessions/{id}", prefix), h.handleDeleteSession)

	// Alerts aggregation
	mux.HandleFunc(fmt.Sprintf("GET %s/alerts", prefix), h.handleGetAlerts)

	// Source test
	mux.HandleFunc(fmt.Sprintf("POST %s/sources/{id}/test", prefix), h.handleTestSource)
}
