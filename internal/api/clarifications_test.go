package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

func setupClarificationsTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test store: %v", err)
	}

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Current: "claude",
			Available: map[string]config.ModelConfig{
				"claude": {
					Provider: "claude",
					Model:    "claude-sonnet-4",
				},
			},
		},
	}

	registry := adapters.NewRegistry()
	services := &core.Services{
		Config:   cfg,
		Store:    sqlStore,
		Adapters: registry,
	}

	return New(services, TestingPolicyEngine(services)), sqlStore
}

// TestClarificationsRoutesParked asserts the parked contract (D-0081): the three
// clarifications HTTP routes are unregistered for launch, so each 404s through
// the mux — and does so regardless of whether the Store sub-service is present.
// Previously the group was conditionally registered when services.Store != nil;
// now it is never registered. Handlers, ClarificationService/Repository, and the
// clarifications table are all retained; only the mux registration is gone.
func TestClarificationsRoutesParked(t *testing.T) {
	parkedRoutes := []struct {
		method string
		path   string
	}{
		{"GET", apiPrefix + "/clarifications"},
		{"POST", apiPrefix + "/clarifications/clar-1/answer"},
		{"POST", apiPrefix + "/clarifications/clar-1/dismiss"},
	}

	t.Run("with store present", func(t *testing.T) {
		server, _ := setupClarificationsTestServer(t)
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		for _, rt := range parkedRoutes {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want %d (parked, D-0081)", rt.method, rt.path, w.Code, http.StatusNotFound)
			}
		}
	})

	t.Run("with nil store", func(t *testing.T) {
		// The group is unregistered regardless of Store presence, so a nil-store
		// server 404s the same way a store-backed one does.
		server := New(&core.Services{Store: nil}, nil)
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		for _, rt := range parkedRoutes {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want %d (parked, D-0081)", rt.method, rt.path, w.Code, http.StatusNotFound)
			}
		}
	})
}

// TestClarificationHandlers_NilStore directly invokes the handler methods with
// a nil store to cover the nil-store guard branches that are otherwise
// unreachable through the HTTP router (routes are not registered when store == nil).
func TestClarificationHandlers_NilStore(t *testing.T) {
	h := &clarificationHandler{storeInst: nil}

	t.Run("list clarifications 503", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/clarifications", nil)
		w := httptest.NewRecorder()
		h.handleListClarifications(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	})

	t.Run("answer clarification 503", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/clarifications/x/answer", nil)
		req.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		h.handleAnswerClarification(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	})

	t.Run("dismiss clarification 503", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/clarifications/x/dismiss", nil)
		req.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		h.handleDismissClarification(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	})
}

// TestClarificationHandlers_StoreError covers the ListPending/GetClarification error
// paths by using a closed store (all DB queries fail with "database is closed").
func TestClarificationHandlers_StoreError(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	sqlStore.Close() // close DB → all queries fail

	h := &clarificationHandler{
		storeInst:            sqlStore,
		clarificationService: nil,
	}

	t.Run("list clarifications 500", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/clarifications", nil)
		w := httptest.NewRecorder()
		h.handleListClarifications(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500 (closed store)", w.Code)
		}
	})

	t.Run("answer clarification 500", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/clarifications/x/answer", nil)
		req.SetPathValue("id", "x")
		req.Body = io.NopCloser(bytes.NewReader([]byte(`{"answer":"yes"}`)))
		w := httptest.NewRecorder()
		h.handleAnswerClarification(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500 (closed store)", w.Code)
		}
	})

	t.Run("dismiss clarification 500", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/clarifications/x/dismiss", nil)
		req.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		h.handleDismissClarification(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500 (closed store)", w.Code)
		}
	})
}

// TestClarificationHandlers_EmptyID covers the id=="" guard branches by calling
// handlers directly without setting a path value (id defaults to "").
func TestClarificationHandlers_EmptyID(t *testing.T) {
	server, storeInst := setupClarificationsTestServer(t)
	h := &clarificationHandler{
		storeInst:            storeInst,
		clarificationService: nil,
	}
	_ = server

	t.Run("dismiss empty id 400", func(t *testing.T) {
		// No SetPathValue("id", ...) → id == "" → 400.
		req := httptest.NewRequest("POST", "/api/v1/clarifications//dismiss", nil)
		w := httptest.NewRecorder()
		h.handleDismissClarification(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("answer empty id 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/clarifications//answer", nil)
		w := httptest.NewRecorder()
		h.handleAnswerClarification(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}
