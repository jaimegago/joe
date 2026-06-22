package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// setupWebUIServer builds a test server with an in-memory store and graph.
func setupWebUIServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	services := &core.Services{
		Config:       &config.Config{},
		Graph:        graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:        sqlStore,
		SessionModel: sessionmodel.NewRepository(sqlStore.DB(), store.DriverSQLite),
		Adapters:     adapters.NewRegistry(),
	}

	srv := New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

// TestWebUIEndpointsRegistered confirms none of the 9 endpoints return 404.
func TestWebUIEndpointsRegistered(t *testing.T) {
	_, mux := setupWebUIServer(t)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/graph"},
		{"GET", "/api/v1/graph/node/some-id"},
		{"GET", "/api/v1/graph/node/some-id/related"},
		{"GET", "/api/v1/sessions"},
		{"POST", "/api/v1/sessions"},
		{"GET", "/api/v1/alerts"},
		// sessions/{id}/messages omitted: owner-scoping now legitimately returns
		// 404 for an unknown/non-owned session, indistinguishable from "route not
		// found" here; covered by TestWebUIChatOwnership_* below.
		// source test omitted: legitimately returns 404 for unknown source,
		// indistinguishable from "route not found" here; covered by TestWebUIComponentTest.
	}

	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			w := doRequest(mux, r.method, r.path, nil)
			if w.Code == http.StatusNotFound {
				t.Errorf("route not registered: %s %s", r.method, r.path)
			}
		})
	}
}

// TestWebUIGetGraph returns empty graph when store has no nodes.
func TestWebUIGetGraph(t *testing.T) {
	_, mux := setupWebUIServer(t)
	w := doRequest(mux, "GET", "/api/v1/graph", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["nodes"]; !ok {
		t.Error("response missing 'nodes' field")
	}
	if _, ok := resp["edges"]; !ok {
		t.Error("response missing 'edges' field")
	}
}

// TestWebUIGetAlerts returns an empty alerts list (stub endpoint).
func TestWebUIGetAlerts(t *testing.T) {
	_, mux := setupWebUIServer(t)
	w := doRequest(mux, "GET", "/api/v1/alerts", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	alerts, ok := resp["alerts"]
	if !ok {
		t.Fatal("response missing 'alerts' field")
	}
	if slice, ok := alerts.([]any); !ok || len(slice) != 0 {
		t.Errorf("expected empty alerts slice, got %v", alerts)
	}
}

// TestWebUICreateAndListSessions tests the session lifecycle.
func TestWebUICreateAndListSessions(t *testing.T) {
	_, mux := setupWebUIServer(t)

	// Create session
	wCreate := doRequest(mux, "POST", "/api/v1/sessions", map[string]any{})
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	var sess map[string]any
	if err := json.NewDecoder(wCreate.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID, ok := sess["id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("session id missing or empty: %v", sess)
	}

	// List sessions — should include the one we just created
	wList := doRequest(mux, "GET", "/api/v1/sessions", nil)
	if wList.Code != http.StatusOK {
		t.Fatalf("list sessions: expected 200, got %d", wList.Code)
	}
	var listResp map[string]any
	if err := json.NewDecoder(wList.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	sessions, ok := listResp["sessions"].([]any)
	if !ok {
		t.Fatalf("sessions field missing or wrong type: %v", listResp)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	// Get messages for that session — should be empty
	wMsgs := doRequest(mux, "GET", "/api/v1/sessions/"+sessionID+"/messages", nil)
	if wMsgs.Code != http.StatusOK {
		t.Fatalf("get messages: expected 200, got %d", wMsgs.Code)
	}
}

// TestWebUIComponentTest returns 404 for unknown source.
func TestWebUIComponentTest(t *testing.T) {
	_, mux := setupWebUIServer(t)
	w := doRequest(mux, "POST", "/api/v1/components/nonexistent/test", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown source, got %d", w.Code)
	}
}

// TestWebUIComponentTest_Success seeds a source and verifies 200 is returned.
func TestWebUIComponentTest_Success(t *testing.T) {
	srv, mux := setupWebUIServer(t)

	if err := srv.services.Store.Components.Create(context.Background(), &store.Component{
		ID: "test-src", Type: "kubernetes", Name: "test cluster",
		Config: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	w := doRequest(mux, "POST", "/api/v1/components/test-src/test", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- nodeToWebUI unit tests ---

func TestNodeToWebUI_BasicFields(t *testing.T) {
	n := graph.Node{
		ID:   "payment-svc",
		Type: "deployment",
		Metadata: map[string]any{
			"name":      "payment-svc",
			"namespace": "prod",
			"cluster":   "us-east-1",
			"status":    "running",
		},
	}
	got := nodeToWebUI(n)

	if got.ID != "payment-svc" {
		t.Errorf("ID = %q, want %q", got.ID, "payment-svc")
	}
	if got.Kind != "deployment" {
		t.Errorf("Kind = %q, want %q", got.Kind, "deployment")
	}
	if got.Name != "payment-svc" {
		t.Errorf("Name = %q, want %q", got.Name, "payment-svc")
	}
	if got.Namespace != "prod" {
		t.Errorf("Namespace = %q, want %q", got.Namespace, "prod")
	}
	if got.Cluster != "us-east-1" {
		t.Errorf("Cluster = %q, want %q", got.Cluster, "us-east-1")
	}
	if got.Status != "running" {
		t.Errorf("Status = %q, want %q", got.Status, "running")
	}
}

func TestNodeToWebUI_FallsBackToIDWhenNameMissing(t *testing.T) {
	n := graph.Node{
		ID:   "my-node",
		Type: "pod",
	}
	got := nodeToWebUI(n)
	if got.Name != "my-node" {
		t.Errorf("Name = %q, want %q (fallback to ID)", got.Name, "my-node")
	}
}

func TestNodeToWebUI_NilMetadataBecomesEmptyMap(t *testing.T) {
	n := graph.Node{ID: "x", Type: "service", Metadata: nil}
	got := nodeToWebUI(n)
	if got.Metadata == nil {
		t.Error("Metadata should be non-nil empty map when source is nil")
	}
}

func TestNodeToWebUI_LabelsExtracted(t *testing.T) {
	n := graph.Node{
		ID:   "svc",
		Type: "service",
		Metadata: map[string]any{
			"labels": map[string]any{"app": "payment", "env": "prod"},
		},
	}
	got := nodeToWebUI(n)
	if got.Labels == nil {
		t.Fatal("Labels should not be nil")
	}
	if got.Labels["app"] != "payment" {
		t.Errorf("Labels[app] = %v, want %q", got.Labels["app"], "payment")
	}
}

func TestNodeToWebUI_NonStringMetadataFieldsIgnored(t *testing.T) {
	n := graph.Node{
		ID:   "svc",
		Type: "service",
		Metadata: map[string]any{
			"name":      42, // int, not string — should be ignored
			"namespace": true,
		},
	}
	got := nodeToWebUI(n)
	// name falls back to node ID when metadata name is not a string
	if got.Name != "svc" {
		t.Errorf("Name = %q, want %q (fallback to ID when non-string)", got.Name, "svc")
	}
	if got.Namespace != "" {
		t.Errorf("Namespace = %q, want empty (non-string)", got.Namespace)
	}
}

// --- edgeToWebUI unit tests ---

func TestEdgeToWebUI(t *testing.T) {
	e := graph.Edge{
		From:     "payment-svc",
		To:       "postgres-db",
		Relation: "stores_in",
	}
	got := edgeToWebUI(e)

	wantID := "payment-svc-stores_in-postgres-db"
	if got.ID != wantID {
		t.Errorf("ID = %q, want %q", got.ID, wantID)
	}
	if got.Source != "payment-svc" {
		t.Errorf("Source = %q, want %q", got.Source, "payment-svc")
	}
	if got.Target != "postgres-db" {
		t.Errorf("Target = %q, want %q", got.Target, "postgres-db")
	}
	if got.Type != "stores_in" {
		t.Errorf("Type = %q, want %q", got.Type, "stores_in")
	}
}

// --- handleGetNode endpoint tests ---

func TestWebUIGetNode_NotFound(t *testing.T) {
	_, mux := setupWebUIServer(t)
	w := doRequest(mux, "GET", "/api/v1/graph/node/nonexistent-id", nil)
	// GetNode returns ErrNodeNotFound (an error), so the handler returns 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing node, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebUIGetNode_Success(t *testing.T) {
	srv, mux := setupWebUIServer(t)

	// Seed a node directly via the graph store.
	ctx := context.Background()
	if err := srv.services.Graph.AddNode(ctx, graph.Node{
		ID:   "test-node",
		Type: "deployment",
		Metadata: map[string]any{
			"name":      "test-node",
			"namespace": "default",
		},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	w := doRequest(mux, "GET", "/api/v1/graph/node/test-node", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var node webUINode
	if err := json.NewDecoder(w.Body).Decode(&node); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if node.ID != "test-node" {
		t.Errorf("ID = %q, want %q", node.ID, "test-node")
	}
	if node.Kind != "deployment" {
		t.Errorf("Kind = %q, want %q", node.Kind, "deployment")
	}
}

// --- handleGetRelatedNodes endpoint tests ---

func TestWebUIGetRelatedNodes_NoRelations(t *testing.T) {
	srv, mux := setupWebUIServer(t)

	ctx := context.Background()
	if err := srv.services.Graph.AddNode(ctx, graph.Node{
		ID: "isolated-node", Type: "service",
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	w := doRequest(mux, "GET", "/api/v1/graph/node/isolated-node/related", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["nodes"]; !ok {
		t.Error("response missing 'nodes' field")
	}
	if _, ok := resp["edges"]; !ok {
		t.Error("response missing 'edges' field")
	}
}

func TestWebUIGetRelatedNodes_WithDepthParam(t *testing.T) {
	srv, mux := setupWebUIServer(t)

	ctx := context.Background()
	if err := srv.services.Graph.AddNode(ctx, graph.Node{ID: "svc-a", Type: "service"}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := srv.services.Graph.AddNode(ctx, graph.Node{ID: "svc-b", Type: "service"}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := srv.services.Graph.AddEdge(ctx, graph.Edge{From: "svc-a", To: "svc-b", Relation: "calls"}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	w := doRequest(mux, "GET", "/api/v1/graph/node/svc-a/related?depth=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- handleGetFullGraph with nodes ---

func TestWebUIGetGraph_WithNodes(t *testing.T) {
	srv, mux := setupWebUIServer(t)

	ctx := context.Background()
	if err := srv.services.Graph.AddNode(ctx, graph.Node{
		ID: "node-1", Type: "deployment",
		Metadata: map[string]any{"name": "node-1"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := srv.services.Graph.AddNode(ctx, graph.Node{
		ID: "node-2", Type: "service",
		Metadata: map[string]any{"name": "node-2"},
	}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := srv.services.Graph.AddEdge(ctx, graph.Edge{
		From: "node-1", To: "node-2", Relation: "exposes",
	}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	w := doRequest(mux, "GET", "/api/v1/graph", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	nodes, _ := resp["nodes"].([]any)
	if len(nodes) != 2 {
		t.Errorf("nodes count = %d, want 2", len(nodes))
	}
	edges, _ := resp["edges"].([]any)
	if len(edges) != 1 {
		t.Errorf("edges count = %d, want 1", len(edges))
	}
}

// TestWebUIListSessions_WithLimitParam verifies the ?limit= query param is honored.
func TestWebUIListSessions_WithLimitParam(t *testing.T) {
	_, mux := setupWebUIServer(t)

	// Create 3 sessions.
	for i := 0; i < 3; i++ {
		w := doRequest(mux, "POST", "/api/v1/sessions", map[string]any{})
		if w.Code != http.StatusCreated {
			t.Fatalf("create session %d: got %d", i, w.Code)
		}
	}

	w := doRequest(mux, "GET", "/api/v1/sessions?limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions with limit: got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	sessions, _ := resp["sessions"].([]any)
	if len(sessions) > 2 {
		t.Errorf("expected at most 2 sessions with limit=2, got %d", len(sessions))
	}
}

// TestWebUIListSessions_NilStore verifies nil store returns empty list (not an error).
func TestWebUIListSessions_NilStore(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	w := doRequest(mux, "GET", "/api/v1/sessions", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with nil store, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["count"].(float64) != 0 {
		t.Errorf("expected count=0, got %v", resp["count"])
	}
}

// TestWebUICreateSession_NilStore verifies nil store returns 503.
func TestWebUICreateSession_NilStore(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	w := doRequest(mux, "POST", "/api/v1/sessions", map[string]any{})
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with nil store, got %d", w.Code)
	}
}

// TestWebUIGetSessionMessages_NilStore verifies nil store returns empty list (not an error).
func TestWebUIGetSessionMessages_NilStore(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	w := doRequest(mux, "GET", "/api/v1/sessions/some-id/messages", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with nil store, got %d", w.Code)
	}
}

// TestWebUITestComponent_NilStore covers the nil-store guard in handleTestComponent.
func TestWebUITestComponent_NilStore(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	// Route "/api/v1/components/{id}/test" is registered; with nil store → 503.
	w := doRequest(mux, "POST", "/api/v1/components/some-id/test", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// TestWebUIGetGraph_NilGraph covers the early return when Graph service is nil.
func TestWebUIGetGraph_NilGraph(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	w := doRequest(mux, "GET", "/api/v1/graph", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with nil graph, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	nodes, _ := resp["nodes"].([]any)
	if len(nodes) != 0 {
		t.Errorf("expected empty nodes, got %d", len(nodes))
	}
}

// TestWebUITestComponent_EmptyID covers the empty-id guard in handleTestComponent directly.
func TestWebUITestComponent_EmptyID(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	h := &webUIHandler{server: srv}

	req := httptest.NewRequest("POST", "/api/v1/components//test", nil)
	// No SetPathValue → PathValue("id") returns "".
	w := httptest.NewRecorder()
	h.handleTestComponent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id, got %d", w.Code)
	}
}

// --- Chat session ownership & isolation (DESIGN-CHAT-SESSIONS.md §11 Phase 1) ---

// reqAsPrincipal issues a request through the mux with the given principal
// injected into the request context — exactly what the production EdgeAuth
// middleware does via rbac.WithPrincipal. An empty principal leaves the context
// unset (resolves to rbac.Unknown).
func reqAsPrincipal(mux *http.ServeMux, method, path, principal string, body any) *httptest.ResponseRecorder {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	if principal != "" {
		req = req.WithContext(rbac.WithPrincipal(req.Context(), rbac.Principal(principal)))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// createSessionAs creates a chat session owned by principal and returns its id.
func createSessionAs(t *testing.T, mux *http.ServeMux, principal string) string {
	t.Helper()
	w := reqAsPrincipal(mux, "POST", "/api/v1/sessions", principal, map[string]any{})
	if w.Code != http.StatusCreated {
		t.Fatalf("create session as %s: got %d: %s", principal, w.Code, w.Body.String())
	}
	var sess map[string]any
	if err := json.NewDecoder(w.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatalf("created session has no id: %v", sess)
	}
	return id
}

// TestWebUIChatOwnership_CrossUserReadIsOpen pins the org-wide read model: a
// session created by alice is READABLE by bob (200) — both its metadata and its
// messages — while writes stay owner-only (covered elsewhere). A non-owner read
// is flagged read_only=true.
func TestWebUIChatOwnership_CrossUserReadIsOpen(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	_, mux := setupWebUIServer(t)

	id := createSessionAs(t, mux, alice)

	owner := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id+"/messages", alice, nil)
	if owner.Code != http.StatusOK {
		t.Errorf("owner read: got %d, want 200", owner.Code)
	}

	// Cross-user read is now allowed.
	other := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id+"/messages", bob, nil)
	if other.Code != http.StatusOK {
		t.Errorf("cross-user messages read: got %d, want 200", other.Code)
	}

	// Metadata read by the non-owner is allowed and flagged read_only=true.
	meta := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id, bob, nil)
	if meta.Code != http.StatusOK {
		t.Fatalf("cross-user metadata read: got %d, want 200", meta.Code)
	}
	var sess map[string]any
	json.NewDecoder(meta.Body).Decode(&sess)
	if sess["read_only"] != true {
		t.Errorf("non-owner read_only = %v, want true", sess["read_only"])
	}

	// The owner's own read is read_only=false.
	ownerMeta := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id, alice, nil)
	var ownerSess map[string]any
	json.NewDecoder(ownerMeta.Body).Decode(&ownerSess)
	if ownerSess["read_only"] != false {
		t.Errorf("owner read_only = %v, want false", ownerSess["read_only"])
	}

	// A genuinely missing session still 404s.
	if w := reqAsPrincipal(mux, "GET", "/api/v1/sessions/does-not-exist", bob, nil); w.Code != http.StatusNotFound {
		t.Errorf("missing session: got %d, want 404", w.Code)
	}
}

// TestWebUIChatList_TeamWideAndMine: the default GET /sessions is the TEAM-WIDE
// list (§12.8 team-wide read) — any authenticated principal sees every session;
// the ?mine=true filter narrows it to the caller's own. This collapses the
// former owner-scoped-vs-shared split into one route.
func TestWebUIChatList_TeamWideAndMine(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	_, mux := setupWebUIServer(t)

	createSessionAs(t, mux, alice)
	createSessionAs(t, mux, alice)
	createSessionAs(t, mux, bob)

	countFor := func(principal, query string) int {
		w := reqAsPrincipal(mux, "GET", "/api/v1/sessions"+query, principal, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("list for %s%s: got %d", principal, query, w.Code)
		}
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		s, _ := resp["sessions"].([]any)
		return len(s)
	}

	// Team-wide: both principals see all three sessions (read is the default).
	if got := countFor(alice, ""); got != 3 {
		t.Errorf("alice team-wide list = %d sessions, want 3", got)
	}
	if got := countFor(bob, ""); got != 3 {
		t.Errorf("bob team-wide list = %d sessions, want 3", got)
	}
	// ?mine=true: each principal sees only their own.
	if got := countFor(alice, "?mine=true"); got != 2 {
		t.Errorf("alice ?mine list = %d sessions, want 2", got)
	}
	if got := countFor(bob, "?mine=true"); got != 1 {
		t.Errorf("bob ?mine list = %d sessions, want 1", got)
	}

	// In the team-wide list, rows the caller does not own are read_only=true and
	// attributed via shared_by; the caller's own rows are read_only=false.
	w := reqAsPrincipal(mux, "GET", "/api/v1/sessions", bob, nil)
	var resp struct {
		Sessions []struct {
			ReadOnly bool   `json:"read_only"`
			SharedBy string `json:"shared_by"`
		} `json:"sessions"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	var ownByBob, ownByOthers int
	for _, s := range resp.Sessions {
		if s.ReadOnly {
			ownByOthers++
			if s.SharedBy != alice {
				t.Errorf("non-owned row shared_by = %q, want %s", s.SharedBy, alice)
			}
		} else {
			ownByBob++
		}
	}
	if ownByBob != 1 || ownByOthers != 2 {
		t.Errorf("bob team-wide split = %d own / %d others, want 1 / 2", ownByBob, ownByOthers)
	}
}

// TestWebUIRenameSession_OwnerOnly covers PATCH /sessions/{id}: the owner can
// rename; a non-owner gets 404 (existence not disclosed); an empty/whitespace
// title is rejected; the rename persists and surfaces in the list.
func TestWebUIRenameSession_OwnerOnly(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	_, mux := setupWebUIServer(t)

	id := createSessionAs(t, mux, alice)

	// Owner renames.
	w := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, alice, map[string]any{"title": "  DB Pool Exhaustion  "})
	if w.Code != http.StatusOK {
		t.Fatalf("owner rename: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var renamed map[string]any
	json.NewDecoder(w.Body).Decode(&renamed)
	if renamed["title"] != "DB Pool Exhaustion" {
		t.Errorf("title = %v, want trimmed %q", renamed["title"], "DB Pool Exhaustion")
	}

	// Empty title is rejected.
	if bad := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, alice, map[string]any{"title": "   "}); bad.Code != http.StatusBadRequest {
		t.Errorf("empty title: got %d, want 400", bad.Code)
	}

	// Non-owner is refused with 404, not 403.
	if other := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, bob, map[string]any{"title": "hijack"}); other.Code != http.StatusNotFound {
		t.Errorf("cross-user rename: got %d, want 404", other.Code)
	}

	// The rename is reflected in the owner's list.
	wl := reqAsPrincipal(mux, "GET", "/api/v1/sessions", alice, nil)
	var listResp struct {
		Sessions []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"sessions"`
	}
	json.NewDecoder(wl.Body).Decode(&listResp)
	if len(listResp.Sessions) != 1 || listResp.Sessions[0].Title != "DB Pool Exhaustion" {
		t.Errorf("list title = %+v, want one session titled %q", listResp.Sessions, "DB Pool Exhaustion")
	}
}

// TestWebUIDeleteSession_OwnerOnly covers DELETE /sessions/{id}: a non-owner is
// refused (404) and the session survives; the owner deletes it (204) and it
// disappears from the list. The chat_messages FK is ON DELETE CASCADE, so the
// delete also expunges the session's messages.
func TestWebUIDeleteSession_OwnerOnly(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	id := createSessionAs(t, mux, alice)
	if _, err := srv.services.SessionModel.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID: "dm1", SessionID: id, Role: "user", Content: "hello",
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// Non-owner cannot soft-delete.
	if other := reqAsPrincipal(mux, "DELETE", "/api/v1/sessions/"+id, bob, nil); other.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete: got %d, want 404", other.Code)
	}
	sess, _ := srv.services.SessionModel.GetSession(ctx, id)
	if sess == nil || sess.TrashedAt != nil {
		t.Fatal("session was trashed by a non-owner")
	}

	// Owner soft-deletes to trash (B007a): 204, but the session is NOT physically
	// gone — it is trashed (recoverable), its transcript is preserved, and it
	// drops out of the active team list.
	if w := reqAsPrincipal(mux, "DELETE", "/api/v1/sessions/"+id, alice, nil); w.Code != http.StatusNoContent {
		t.Fatalf("owner soft-delete: got %d, want 204", w.Code)
	}
	sess, _ = srv.services.SessionModel.GetSession(ctx, id)
	if sess == nil {
		t.Fatal("session physically removed by a soft-delete — must be trashed, not purged")
	}
	if sess.TrashedAt == nil || sess.TrashedBy == nil || *sess.TrashedBy != alice {
		t.Errorf("after soft-delete: trashed_at=%v trashed_by=%v, want set with trashed_by=alice", sess.TrashedAt, sess.TrashedBy)
	}
	// The transcript is preserved (soft-delete is not a purge).
	if msgs, _ := srv.services.SessionModel.ListChatMessages(ctx, id); len(msgs) != 1 {
		t.Errorf("messages after soft-delete = %d, want 1 (preserved, not cascade-deleted)", len(msgs))
	}
	// Trashed session is removed from the active team list.
	list := reqAsPrincipal(mux, "GET", "/api/v1/sessions", alice, nil)
	var listed struct {
		Sessions []map[string]any `json:"sessions"`
	}
	json.NewDecoder(list.Body).Decode(&listed)
	for _, s := range listed.Sessions {
		if s["id"] == id {
			t.Error("trashed session still appears in the active team list")
		}
	}

	// Owner restores it back to active.
	if w := reqAsPrincipal(mux, "POST", "/api/v1/sessions/"+id+"/restore", alice, nil); w.Code != http.StatusOK {
		t.Fatalf("owner restore: got %d, want 200", w.Code)
	}
	sess, _ = srv.services.SessionModel.GetSession(ctx, id)
	if sess == nil || sess.TrashedAt != nil || sess.TrashedBy != nil || sess.PurgeAfter != nil {
		t.Errorf("after restore: lifecycle columns not cleared: %+v", sess)
	}
}

// TestWebUIShareSession_ReadOpenWriteOwnerOnly pins the org-wide model: any
// authenticated user can READ any session (metadata + messages), flagged
// read_only=true for a non-owner and without leaking creator_principal; but
// WRITES (rename) stay owner-only.
func TestWebUIShareSession_ReadOpenWriteOwnerOnly(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	id := createSessionAs(t, mux, alice)
	if _, err := srv.services.SessionModel.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID: "pm1", SessionID: id, Role: "user", Content: "anyone can read this",
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// bob (a non-owner) can read the metadata, flagged read-only, no owner leak.
	got := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id, bob, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("cross-user GET session: got %d, want 200", got.Code)
	}
	var sess map[string]any
	json.NewDecoder(got.Body).Decode(&sess)
	if sess["read_only"] != true {
		t.Errorf("non-owner read_only = %v, want true", sess["read_only"])
	}
	if _, leaked := sess["creator_principal"]; leaked {
		t.Error("session response leaked creator_principal")
	}
	if msgs := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id+"/messages", bob, nil); msgs.Code != http.StatusOK {
		t.Fatalf("cross-user GET messages: got %d, want 200", msgs.Code)
	}

	// Owner GET reports read_only=false explicitly (present, not omitted) — the
	// client gates owner-only controls on this positive signal.
	ownerGet := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+id, alice, nil)
	var ownerSess map[string]any
	json.NewDecoder(ownerGet.Body).Decode(&ownerSess)
	if ro, present := ownerSess["read_only"]; !present || ro != false {
		t.Errorf("owner read_only present=%v value=%v, want present=true value=false", present, ro)
	}

	// Writes stay owner-only: bob cannot rename alice's session (404), alice can.
	if w := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, bob, map[string]any{"title": "hijack"}); w.Code != http.StatusNotFound {
		t.Errorf("non-owner rename: got %d, want 404", w.Code)
	}
	if w := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, alice, map[string]any{"title": "Alice's title"}); w.Code != http.StatusOK {
		t.Errorf("owner rename: got %d, want 200", w.Code)
	}
}

// TestWebUIUpdateSession_Validation covers PATCH validation now that title is
// the only mutable field: a non-owner is refused (404), an empty/absent title is
// rejected (400), and a missing session 404s.
func TestWebUIUpdateSession_Validation(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	_, mux := setupWebUIServer(t)

	id := createSessionAs(t, mux, alice)

	if w := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, bob, map[string]any{"title": "x"}); w.Code != http.StatusNotFound {
		t.Errorf("non-owner rename: got %d, want 404", w.Code)
	}
	if w := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, alice, map[string]any{"title": "   "}); w.Code != http.StatusBadRequest {
		t.Errorf("empty title: got %d, want 400", w.Code)
	}
	if w := reqAsPrincipal(mux, "PATCH", "/api/v1/sessions/"+id, alice, map[string]any{}); w.Code != http.StatusBadRequest {
		t.Errorf("PATCH with no title: got %d, want 400", w.Code)
	}
	if w := reqAsPrincipal(mux, "GET", "/api/v1/sessions/does-not-exist", alice, nil); w.Code != http.StatusNotFound {
		t.Errorf("GET missing session: got %d, want 404", w.Code)
	}
}

// TestWebUITeamWideListMembership covers the team-wide read model across three
// principals (§12.8): the default GET /sessions returns EVERY session to any
// caller (rows the caller doesn't own flagged read_only + shared_by, no
// creator_principal leak), while ?mine=true returns only the caller's own.
func TestWebUITeamWideListMembership(t *testing.T) {
	const alice, bob, carol = "user:alice@example.com", "user:bob@example.com", "user:carol@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	aliceID := createSessionAs(t, mux, alice)
	if _, err := srv.services.SessionModel.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID: "sm1", SessionID: aliceID, Role: "user", Content: "team-wide?",
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	bobID := createSessionAs(t, mux, bob)

	// carol (owning nothing) sees BOTH alice's and bob's sessions in the team-wide
	// list; every row is read_only with no creator_principal leak.
	carolList := listSessionIDs(t, mux, carol, "")
	if !slices.Contains(carolList.ids, aliceID) || !slices.Contains(carolList.ids, bobID) || len(carolList.ids) != 2 {
		t.Errorf("carol team-wide list = %v, want both %s and %s", carolList.ids, aliceID, bobID)
	}
	for _, row := range carolList.rows {
		if row["read_only"] != true {
			t.Errorf("carol row %v read_only = %v, want true (owns nothing)", row["id"], row["read_only"])
		}
		if row["shared_by"] != alice && row["shared_by"] != bob {
			t.Errorf("carol row shared_by = %v, want one of the owners", row["shared_by"])
		}
		if _, leaked := row["creator_principal"]; leaked {
			t.Error("team-wide row leaked creator_principal field")
		}
	}

	// bob's team-wide list contains alice's session flagged read_only + shared_by,
	// and his OWN session flagged read_only=false.
	bobList := listSessionIDs(t, mux, bob, "")
	if len(bobList.ids) != 2 {
		t.Fatalf("bob team-wide list = %v, want 2", bobList.ids)
	}
	for _, row := range bobList.rows {
		if row["id"] == aliceID {
			if row["read_only"] != true || row["shared_by"] != alice {
				t.Errorf("bob's view of alice's row read_only=%v shared_by=%v, want true / %s",
					row["read_only"], row["shared_by"], alice)
			}
		}
		if row["id"] == bobID && row["read_only"] != false {
			t.Errorf("bob's own row read_only = %v, want false", row["read_only"])
		}
	}

	// ?mine=true: alice sees only her own, bob only his own.
	if mine := listSessionIDs(t, mux, alice, "?mine=true"); len(mine.ids) != 1 || mine.ids[0] != aliceID {
		t.Errorf("alice ?mine list = %v, want [%s]", mine.ids, aliceID)
	}
	if mine := listSessionIDs(t, mux, bob, "?mine=true"); len(mine.ids) != 1 || mine.ids[0] != bobID {
		t.Errorf("bob ?mine list = %v, want [%s]", mine.ids, bobID)
	}
}

// sessionList is the decoded GET /sessions response used by listSessionIDs.
type sessionList struct {
	ids  []string
	rows []map[string]any
}

// listSessionIDs fetches GET /sessions<query> as principal and returns the row
// ids and the raw rows.
func listSessionIDs(t *testing.T, mux *http.ServeMux, principal, query string) sessionList {
	t.Helper()
	w := reqAsPrincipal(mux, "GET", "/api/v1/sessions"+query, principal, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list for %s%s: got %d, want 200", principal, query, w.Code)
	}
	var resp struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	out := sessionList{rows: resp.Sessions}
	for _, s := range resp.Sessions {
		if id, ok := s["id"].(string); ok {
			out.ids = append(out.ids, id)
		}
	}
	return out
}

// TestWebUILinkIncident covers POST /sessions/{id}/link-incident (Phase 4):
// 409 when no incident is active; owner-only (non-owner 404); the link promotes
// the session to type='investigation' and records linked_incident_id, surfaced
// in the response; a missing session 404s.
func TestWebUILinkIncident(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	chatID := createSessionAs(t, mux, alice)

	// No active incident yet → 409.
	if w := reqAsPrincipal(mux, "POST", "/api/v1/sessions/"+chatID+"/link-incident", alice, nil); w.Code != http.StatusConflict {
		t.Fatalf("link with no active incident: got %d, want 409", w.Code)
	}

	// Seed an active incident (creator is irrelevant to the chat session's owner-check).
	declared := sessionmodel.IncidentStateDeclared
	if _, err := srv.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "inc-1", Type: sessionmodel.SessionTypeIncident, IncidentState: &declared,
		CreatorPrincipal: bob,
	}); err != nil {
		t.Fatalf("seed incident: %v", err)
	}

	// Non-owner of the chat session cannot link it — 404, not 403.
	if w := reqAsPrincipal(mux, "POST", "/api/v1/sessions/"+chatID+"/link-incident", bob, nil); w.Code != http.StatusNotFound {
		t.Errorf("cross-user link: got %d, want 404", w.Code)
	}

	// Owner links it to the active incident.
	w := reqAsPrincipal(mux, "POST", "/api/v1/sessions/"+chatID+"/link-incident", alice, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("owner link: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var linked map[string]any
	json.NewDecoder(w.Body).Decode(&linked)
	if linked["linked_incident_id"] != "inc-1" {
		t.Errorf("linked_incident_id = %v, want inc-1", linked["linked_incident_id"])
	}

	// The promotion + link persisted.
	sess, _ := srv.services.SessionModel.GetSession(ctx, chatID)
	if sess == nil || sess.Type != sessionmodel.SessionTypeDefault {
		t.Errorf("type = %v, want investigation", sess)
	}
	if sess == nil || sess.LinkedIncidentID == nil || *sess.LinkedIncidentID != "inc-1" {
		t.Errorf("linked_incident_id not persisted: %v", sess)
	}

	// A missing session 404s.
	if w := reqAsPrincipal(mux, "POST", "/api/v1/sessions/does-not-exist/link-incident", alice, nil); w.Code != http.StatusNotFound {
		t.Errorf("link missing session: got %d, want 404", w.Code)
	}
}

// TestWebUIGetSession_LinkedIncidentTitle verifies the per-id GET resolves the
// linked incident MASTER's title into linked_incident_title (defect 2), so the
// chat header can render a navigable "Linked to «master title»" badge without a
// second request. It survives the master's resolution (the link pointer does).
func TestWebUIGetSession_LinkedIncidentTitle(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	chatID := createSessionAs(t, mux, alice)

	// Seed a TITLED incident master and link the chat session to it directly
	// (the link store call, bypassing the active-incident guard so this test
	// exercises projection, not the link route).
	title := "DB pool exhaustion"
	declared := sessionmodel.IncidentStateDeclared
	if _, err := srv.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "inc-titled", Type: sessionmodel.SessionTypeIncident, IncidentState: &declared,
		CreatorPrincipal: bob, Title: &title,
	}); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	if err := srv.services.SessionModel.LinkSessionToIncident(ctx, chatID, "inc-titled"); err != nil {
		t.Fatalf("link session: %v", err)
	}

	got := reqAsPrincipal(mux, "GET", "/api/v1/sessions/"+chatID, alice, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get session: got %d, want 200: %s", got.Code, got.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(got.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["linked_incident_id"] != "inc-titled" {
		t.Errorf("linked_incident_id = %v, want inc-titled", body["linked_incident_id"])
	}
	if body["linked_incident_title"] != title {
		t.Errorf("linked_incident_title = %v, want %q", body["linked_incident_title"], title)
	}
}

// TestWebUIListSessions_IncidentProjection is the LIST sibling of
// TestWebUIGetSession_LinkedIncidentTitle: it decodes the real GET /sessions
// JSON and asserts the P0 read-model projection
// (docs/DESIGN-SESSIONS-VIEW.md §4) — the incident_involved flag and, on a
// linked child, the master id + title — so the bare-badge defect is closed on
// the list, not just the per-id GET.
func TestWebUIListSessions_IncidentProjection(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	// A plain conversation (incident-free), a titled incident master, and a
	// child linked to that master.
	plain := createSessionAs(t, mux, alice)
	child := createSessionAs(t, mux, alice)
	title := "DB pool exhaustion"
	declared := sessionmodel.IncidentStateDeclared
	if _, err := srv.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "inc-master", Type: sessionmodel.SessionTypeIncident, IncidentState: &declared,
		CreatorPrincipal: bob, Title: &title,
	}); err != nil {
		t.Fatalf("seed incident master: %v", err)
	}
	if err := srv.services.SessionModel.LinkSessionToIncident(ctx, child, "inc-master"); err != nil {
		t.Fatalf("link child: %v", err)
	}

	w := reqAsPrincipal(mux, "GET", "/api/v1/sessions", alice, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Sessions []struct {
			ID                  string `json:"id"`
			Type                string `json:"type"`
			IncidentInvolved    bool   `json:"incident_involved"`
			LinkedIncidentID    string `json:"linked_incident_id"`
			LinkedIncidentTitle string `json:"linked_incident_title"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byID := map[string]struct {
		ID                  string `json:"id"`
		Type                string `json:"type"`
		IncidentInvolved    bool   `json:"incident_involved"`
		LinkedIncidentID    string `json:"linked_incident_id"`
		LinkedIncidentTitle string `json:"linked_incident_title"`
	}{}
	for _, s := range resp.Sessions {
		byID[s.ID] = s
	}

	// The plain conversation: incident-free, no master.
	if got := byID[plain]; got.IncidentInvolved {
		t.Errorf("plain session incident_involved = true, want false")
	}
	// The master: incident-involved by type, no linked master of its own.
	master, ok := byID["inc-master"]
	if !ok {
		t.Fatalf("master not in list")
	}
	if !master.IncidentInvolved {
		t.Errorf("master incident_involved = false, want true")
	}
	if master.LinkedIncidentID != "" {
		t.Errorf("master linked_incident_id = %q, want empty", master.LinkedIncidentID)
	}
	// The child: incident-involved by link, carrying the master id AND title so
	// the badge is titled, not bare.
	c, ok := byID[child]
	if !ok {
		t.Fatalf("child not in list")
	}
	if !c.IncidentInvolved {
		t.Errorf("child incident_involved = false, want true")
	}
	if c.LinkedIncidentID != "inc-master" {
		t.Errorf("child linked_incident_id = %q, want inc-master", c.LinkedIncidentID)
	}
	if c.LinkedIncidentTitle != title {
		t.Errorf("child linked_incident_title = %q, want %q", c.LinkedIncidentTitle, title)
	}
}

// TestWebUIChatOwnership_MessagesRoundTrip is the owner happy-path: messages
// persisted to a session come back in order, in the legacy flat JSON shape the
// chat UI consumes (numeric id from seq, role, content), and only to the owner.
func TestWebUIChatOwnership_MessagesRoundTrip(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	srv, mux := setupWebUIServer(t)
	ctx := context.Background()

	if _, err := srv.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "s-rt", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: alice,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, m := range []sessionmodel.ChatMessage{
		{ID: "m1", SessionID: "s-rt", Role: "user", Content: "hello"},
		{ID: "m2", SessionID: "s-rt", Role: "assistant", Content: "hi there"},
	} {
		if _, err := srv.services.SessionModel.AddChatMessage(ctx, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	w := reqAsPrincipal(mux, "GET", "/api/v1/sessions/s-rt/messages", alice, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("owner messages: got %d", w.Code)
	}
	var resp struct {
		Messages []struct {
			ID      int    `json:"id"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Count int `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 || len(resp.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", resp.Count)
	}
	if resp.Messages[0].Role != "user" || resp.Messages[0].Content != "hello" {
		t.Errorf("message[0] = {%q,%q}, want {user,hello}", resp.Messages[0].Role, resp.Messages[0].Content)
	}
	if resp.Messages[1].Role != "assistant" || resp.Messages[1].Content != "hi there" {
		t.Errorf("message[1] = {%q,%q}, want {assistant,hi there}", resp.Messages[1].Role, resp.Messages[1].Content)
	}
	// id carries the per-session seq (1-based), so the order key is monotonic.
	if resp.Messages[0].ID != 1 || resp.Messages[1].ID != 2 {
		t.Errorf("message ids = %d,%d, want 1,2 (seq)", resp.Messages[0].ID, resp.Messages[1].ID)
	}

	// bob (a non-owner) can read the populated session too (org-wide read model).
	if other := reqAsPrincipal(mux, "GET", "/api/v1/sessions/s-rt/messages", bob, nil); other.Code != http.StatusOK {
		t.Errorf("cross-user read of populated session: got %d, want 200", other.Code)
	}
}
