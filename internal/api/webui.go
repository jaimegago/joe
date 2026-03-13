package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
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
	if h.server.services.Graph == nil {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}, "edges": []any{}})
		return
	}

	subgraph, err := h.server.services.Graph.ListAll(r.Context())
	if err != nil {
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

	n, err := h.server.services.Graph.GetNode(r.Context(), nodeID)
	if err != nil {
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

	subgraph, err := h.server.services.Graph.Related(r.Context(), nodeID, depth)
	if err != nil {
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

// handleListSessions returns recent sessions.
func (h *webUIHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Store == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "count": 0})
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	sessions, err := h.server.services.Store.Sessions.ListRecent(r.Context(), limit)
	if err != nil {
		writeInternalError(w, err, "list sessions")
		return
	}
	if sessions == nil {
		sessions = []*store.Session{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleCreateSession creates a new session.
func (h *webUIHandler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Store == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "store not available")
		return
	}

	session := &store.Session{
		ID:        uid.New(),
		StartedAt: time.Now().UTC(),
	}

	if err := h.server.services.Store.Sessions.Create(r.Context(), session); err != nil {
		writeInternalError(w, err, "create session")
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

// handleGetSessionMessages returns messages for a session.
func (h *webUIHandler) handleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Store == nil {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []any{}, "count": 0})
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing session id")
		return
	}

	messages, err := h.server.services.Store.Sessions.GetMessages(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "get session messages")
		return
	}
	if messages == nil {
		messages = []*store.SessionMessage{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"count":    len(messages),
	})
}

// chatMessageRequest is the request body for POST /api/v1/chat.
type chatMessageRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// chatMessageResponse is the response for POST /api/v1/chat.
type chatMessageResponse struct {
	Message   *store.SessionMessage `json:"message"`
	ToolCalls []llm.ToolCall        `json:"tool_calls,omitempty"`
}

// handleChat processes a chat message using the LLM.
func (h *webUIHandler) handleChat(w http.ResponseWriter, r *http.Request) {
	if h.server.services.LLM == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM not available")
		return
	}

	var req chatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "message is required")
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uid.New()
	}

	// Ensure session exists
	if h.server.services.Store != nil {
		sess, _ := h.server.services.Store.Sessions.Get(r.Context(), sessionID)
		if sess == nil {
			_ = h.server.services.Store.Sessions.Create(r.Context(), &store.Session{
				ID:        sessionID,
				StartedAt: time.Now().UTC(),
			})
		}
	}

	// Load message history for context
	var history []llm.Message
	if h.server.services.Store != nil {
		msgs, err := h.server.services.Store.Sessions.GetMessages(r.Context(), sessionID)
		if err == nil {
			for _, m := range msgs {
				if m.Role == "user" || m.Role == "assistant" {
					history = append(history, llm.Message{
						Role:    m.Role,
						Content: m.Content,
					})
				}
			}
		}
	}

	// Get graph summary for context
	graphSummary := ""
	if h.server.services.Graph != nil {
		if summary, err := h.server.services.Graph.Summary(r.Context()); err == nil {
			graphSummary = fmt.Sprintf(
				"Infrastructure graph: %d nodes, %d edges. Node types: %v",
				summary.NodeCount, summary.EdgeCount, summary.NodesByType,
			)
		}
	}

	systemPrompt := `You are Joe, an AI-powered infrastructure copilot running in the Web UI. Help the user understand, debug, and operate their infrastructure.

IMPORTANT LIMITATIONS: The Web UI connects to joecored (the Joe Core daemon) which has access to configured remote sources (Kubernetes clusters, cloud providers, observability tools, etc.). It does NOT have access to the user's local machine, local files, local kubectl context, or local Kind/minikube clusters.

When a user asks about anything local — for example "check my local cluster", "check my local pods", "run kubectl", "read a file on my machine" — you must:
1. Explain that the Web UI cannot access local resources directly.
2. Recommend they use the Joe CLI (REPL) instead: running ` + "`joe`" + ` in a terminal gives them a local agent that can run kubectl, read files, and execute commands on their machine.
3. Keep your explanation brief and friendly.

For remote infrastructure that joecored is connected to, answer normally using the graph context below.`

	if graphSummary != "" {
		systemPrompt += "\n\nCurrent infrastructure context:\n" + graphSummary
	}

	// Append user message
	history = append(history, llm.Message{
		Role:    "user",
		Content: req.Message,
	})

	// Call LLM
	llmResp, err := h.server.services.LLM.Chat(r.Context(), llm.ChatRequest{
		SystemPrompt: systemPrompt,
		Messages:     history,
		MaxTokens:    2048,
	})
	if err != nil {
		slog.Error("chat LLM call failed", "error", err)
		writeInternalError(w, err, "chat")
		return
	}

	// Store user message
	userMsg := &store.SessionMessage{
		SessionID: sessionID,
		Role:      "user",
		Content:   req.Message,
		CreatedAt: time.Now().UTC(),
	}
	if h.server.services.Store != nil {
		_ = h.server.services.Store.Sessions.AddMessage(r.Context(), userMsg)
	}

	// Store assistant response
	assistantMsg := &store.SessionMessage{
		SessionID: sessionID,
		Role:      "assistant",
		Content:   llmResp.Content,
		CreatedAt: time.Now().UTC(),
	}
	if h.server.services.Store != nil {
		_ = h.server.services.Store.Sessions.AddMessage(r.Context(), assistantMsg)
	}

	writeJSON(w, http.StatusOK, chatMessageResponse{
		Message:   assistantMsg,
		ToolCalls: llmResp.ToolCalls,
	})
}

// handleGetAlerts returns an aggregated list of active alerts (stub).
func (h *webUIHandler) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	// TODO: aggregate from Alertmanager/Grafana sources
	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": []any{},
		"count":  0,
	})
}

// handleTestSource tests whether a source connection is healthy.
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

	src, err := h.server.services.Store.Sources.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source for test")
		return
	}
	if src == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("source %q exists with status %q", src.ID, src.Status),
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
	mux.HandleFunc(fmt.Sprintf("GET %s/sessions/{id}/messages", prefix), h.handleGetSessionMessages)

	// Chat
	mux.HandleFunc(fmt.Sprintf("POST %s/chat", prefix), h.handleChat)

	// Alerts aggregation
	mux.HandleFunc(fmt.Sprintf("GET %s/alerts", prefix), h.handleGetAlerts)

	// Source test
	mux.HandleFunc(fmt.Sprintf("POST %s/sources/{id}/test", prefix), h.handleTestSource)
}
