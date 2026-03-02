package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
	_ "github.com/mattn/go-sqlite3"
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

	sqlStore, err := store.New(":memory:?_foreign_keys=on", nil)
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
