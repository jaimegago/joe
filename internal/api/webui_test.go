package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// stubLLM is a minimal LLM adapter that returns a canned response.
type stubLLM struct{}

func (s *stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: "stub response"}, nil
}

func (s *stubLLM) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (s *stubLLM) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2}, nil
}

// setupWebUIServer builds a test server with an in-memory store and graph.
// Pass withLLM=true to wire up a stub LLM (required by the chat endpoint).
func setupWebUIServer(t *testing.T, withLLM bool) (*Server, *http.ServeMux) {
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
		Config:   &config.Config{},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
	}
	if withLLM {
		services.LLM = &stubLLM{}
	}

	srv := New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

// TestWebUIEndpointsRegistered confirms none of the 9 endpoints return 404.
func TestWebUIEndpointsRegistered(t *testing.T) {
	_, mux := setupWebUIServer(t, false)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/graph"},
		{"GET", "/api/v1/graph/node/some-id"},
		{"GET", "/api/v1/graph/node/some-id/related"},
		{"GET", "/api/v1/sessions"},
		{"POST", "/api/v1/sessions"},
		{"GET", "/api/v1/sessions/some-id/messages"},
		{"POST", "/api/v1/chat"},
		{"GET", "/api/v1/alerts"},
		// source test omitted: legitimately returns 404 for unknown source,
		// indistinguishable from "route not found" here; covered by TestWebUISourceTest.
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
	_, mux := setupWebUIServer(t, false)
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
	_, mux := setupWebUIServer(t, false)
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
	_, mux := setupWebUIServer(t, false)

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

// TestWebUIChatRequiresLLM is a regression test for the bug where services.LLM
// was never assigned in main.go, causing every chat request to return 503.
func TestWebUIChatRequiresLLM(t *testing.T) {
	t.Run("503 when LLM not wired", func(t *testing.T) {
		_, mux := setupWebUIServer(t, false) // withLLM=false
		w := doRequest(mux, "POST", "/api/v1/chat", map[string]any{
			"session_id": "test-session",
			"message":    "hello",
		})
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 when LLM is nil, got %d", w.Code)
		}
	})

	t.Run("200 when LLM is wired", func(t *testing.T) {
		_, mux := setupWebUIServer(t, true) // withLLM=true
		w := doRequest(mux, "POST", "/api/v1/chat", map[string]any{
			"session_id": "test-session",
			"message":    "hello",
		})
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 with LLM wired, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode chat response: %v", err)
		}
		if resp["message"] == nil {
			t.Error("chat response missing 'message' field")
		}
	})

	t.Run("400 when message is empty", func(t *testing.T) {
		_, mux := setupWebUIServer(t, true)
		w := doRequest(mux, "POST", "/api/v1/chat", map[string]any{
			"session_id": "test-session",
			"message":    "",
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty message, got %d", w.Code)
		}
	})
}

// TestWebUISourceTest returns 404 for unknown source.
func TestWebUISourceTest(t *testing.T) {
	_, mux := setupWebUIServer(t, false)
	w := doRequest(mux, "POST", "/api/v1/sources/nonexistent/test", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown source, got %d", w.Code)
	}
}

// TestWebUISourceTest_Success seeds a source and verifies 200 is returned.
func TestWebUISourceTest_Success(t *testing.T) {
	srv, mux := setupWebUIServer(t, false)

	if err := srv.services.Store.Sources.Create(context.Background(), &store.Source{
		ID: "test-src", Type: "kubernetes", Name: "test cluster",
		Config: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	w := doRequest(mux, "POST", "/api/v1/sources/test-src/test", nil)
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
	_, mux := setupWebUIServer(t, false)
	w := doRequest(mux, "GET", "/api/v1/graph/node/nonexistent-id", nil)
	// GetNode returns ErrNodeNotFound (an error), so the handler returns 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing node, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebUIGetNode_Success(t *testing.T) {
	srv, mux := setupWebUIServer(t, false)

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
	srv, mux := setupWebUIServer(t, false)

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
	srv, mux := setupWebUIServer(t, false)

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
	srv, mux := setupWebUIServer(t, false)

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
	_, mux := setupWebUIServer(t, false)

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

// TestWebUITestSource_NilStore covers the nil-store guard in handleTestSource.
func TestWebUITestSource_NilStore(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	// Route "/api/v1/sources/{id}/test" is registered; with nil store → 503.
	w := doRequest(mux, "POST", "/api/v1/sources/some-id/test", nil)
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

// TestWebUITestSource_EmptyID covers the empty-id guard in handleTestSource directly.
func TestWebUITestSource_EmptyID(t *testing.T) {
	srv := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	h := &webUIHandler{server: srv}

	req := httptest.NewRequest("POST", "/api/v1/sources//test", nil)
	// No SetPathValue → PathValue("id") returns "".
	w := httptest.NewRecorder()
	h.handleTestSource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id, got %d", w.Code)
	}
}

// TestWebUIChat_EmptySessionID covers the session-id auto-generation path.
func TestWebUIChat_EmptySessionID(t *testing.T) {
	_, mux := setupWebUIServer(t, true) // withLLM=true

	// Omit session_id — handler should auto-generate one.
	w := doRequest(mux, "POST", "/api/v1/chat", map[string]any{
		"message": "hello",
	})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when session_id omitted, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebUIChat_InvalidJSON covers the JSON decode error path in handleChat.
func TestWebUIChat_InvalidJSON(t *testing.T) {
	_, mux := setupWebUIServer(t, true) // withLLM=true

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}
