package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "github.com/mattn/go-sqlite3"
)

// setupFullTestServer creates a test server with full store (graph + sources tables).
func setupFullTestServer(t *testing.T) *api.Server {
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

	return api.New(services)
}

func TestHandleListSources_Empty(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/sources", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(body["count"].(float64)) != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestHandleCreateSource_MissingFields(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader(`{"id":"test"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateSource_InvalidJSON(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader(`{bad`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateSource_InvalidType(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader(`{"id":"src-1","type":"nope","name":"bad","config":{}}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var response struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "invalid_source" {
		t.Errorf("error code: got %q, want %q", response.Error, "invalid_source")
	}
}

func TestHandleGetSource_NotFound(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/sources/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteSource_NotFound(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/v1/sources/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// setupTestServerWithStore creates a test server and returns the underlying store.
func setupTestServerWithStore(t *testing.T) (*api.Server, *store.Store, *http.ServeMux) {
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

	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return server, sqlStore, mux
}

func TestHandleCreateSource_DuplicateSource(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	// Pre-insert a source directly so the duplicate check triggers
	sqlStore.Sources.Create(context.Background(), &store.Source{
		ID:     "src-dup",
		Type:   "kubernetes",
		Name:   "existing cluster",
		Config: json.RawMessage(`{}`),
	})

	body := `{"id":"src-dup","type":"kubernetes","name":"cluster"}`
	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "invalid_request" {
		t.Errorf("error code = %v, want invalid_request", resp["error"])
	}
}

func TestHandleCreateSource_GitConnectFails(t *testing.T) {
	// Empty config causes git adapter to fail (url is required) -> covers writeBadRequest
	_, _, mux := setupTestServerWithStore(t)

	body := `{"id":"git-1","type":"git","name":"test repo","config":{}}`
	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (git connect should fail with empty config)", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetSource_Success(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	sqlStore.Sources.Create(context.Background(), &store.Source{
		ID:     "test-src",
		Type:   "git",
		Name:   "test repo",
		Config: json.RawMessage(`{}`),
	})

	req := httptest.NewRequest("GET", "/api/v1/sources/test-src", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] != "test-src" {
		t.Errorf("id = %v, want test-src", resp["id"])
	}
}

func TestHandleDeleteSource_Success(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	sqlStore.Sources.Create(context.Background(), &store.Source{
		ID:     "del-src",
		Type:   "kubernetes",
		Name:   "to delete",
		Config: json.RawMessage(`{}`),
	})

	req := httptest.NewRequest("DELETE", "/api/v1/sources/del-src", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Verify source is gone
	src, _ := sqlStore.Sources.Get(context.Background(), "del-src")
	if src != nil {
		t.Error("source should have been deleted")
	}
}
