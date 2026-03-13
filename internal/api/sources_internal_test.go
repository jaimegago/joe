package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// newClosedStore creates a migrated in-memory store and immediately closes it
// so that subsequent DB queries return "sql: database is closed".
func newClosedStore(t *testing.T) *store.Store {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	sqlStore.Close()
	return sqlStore
}

// TestHandleListSources_StoreError triggers the error path in handleListSources
// by using a closed store (all DB queries fail with "database is closed").
func TestHandleListSources_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	req := httptest.NewRequest("GET", "/api/v1/sources", nil)
	w := httptest.NewRecorder()
	s.handleListSources(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleSummary_GraphError triggers the error path in handleSummary
// by using a graph store whose underlying DB is closed.
func TestHandleSummary_GraphError(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	graphStore := graph.NewSQLiteStore(sqlStore.DB(), nil)
	sqlStore.Close() // close DB → graph queries now fail

	h := &graphHandler{graph: graphStore}
	req := httptest.NewRequest("GET", "/api/v1/graph/summary", nil)
	w := httptest.NewRecorder()
	h.handleSummary(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed DB, got %d", w.Code)
	}
}

// TestHandleGetFullGraph_GraphError covers the ListAll error path when the graph DB is closed.
func TestHandleGetFullGraph_GraphError(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	graphStore := graph.NewSQLiteStore(sqlStore.DB(), nil)
	sqlStore.Close() // close DB → ListAll queries fail

	s := New(&core.Services{Config: &config.Config{}, Graph: graphStore, Adapters: adapters.NewRegistry()})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	w := httptest.NewRecorder()
	h.handleGetFullGraph(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed DB, got %d", w.Code)
	}
}

// TestHandleTestSource_StoreError covers the Sources.Get error path in handleTestSource.
func TestHandleTestSource_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("POST", "/api/v1/sources/some-id/test", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	h.handleTestSource(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleCreateSession_StoreError covers the Sessions.Create error path.
func TestHandleCreateSession_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("POST", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.handleCreateSession(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleListSessions_StoreError covers the Sessions.List error path.
func TestHandleListSessions_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.handleListSessions(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleGetSessionMessages_StoreError covers the Sessions.GetMessages error path.
func TestHandleGetSessionMessages_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("GET", "/api/v1/sessions/my-session/messages", nil)
	req.SetPathValue("id", "my-session")
	w := httptest.NewRecorder()
	h.handleGetSessionMessages(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleDeleteSource_StoreError covers the Sources.Get error path in handleDeleteSource.
func TestHandleDeleteSource_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	req := httptest.NewRequest("DELETE", "/api/v1/sources/some-id", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	s.handleDeleteSource(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleGetSource_StoreError covers the Sources.Get error path in handleGetSource.
func TestHandleGetSource_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:   &config.Config{},
		Store:    newClosedStore(t),
		Adapters: adapters.NewRegistry(),
	})
	req := httptest.NewRequest("GET", "/api/v1/sources/some-id", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	s.handleGetSource(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleGetSource_EmptyID covers the id=="" guard by calling the handler
// directly without setting a path value (defaults to "").
func TestHandleGetSource_EmptyID(t *testing.T) {
	s := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	req := httptest.NewRequest("GET", "/api/v1/sources/", nil)
	// Do NOT call req.SetPathValue — PathValue("id") returns "".
	w := httptest.NewRecorder()
	s.handleGetSource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id, got %d", w.Code)
	}
}

// TestHandleDeleteSource_EmptyID covers the id=="" guard in handleDeleteSource.
func TestHandleDeleteSource_EmptyID(t *testing.T) {
	s := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	req := httptest.NewRequest("DELETE", "/api/v1/sources/", nil)
	w := httptest.NewRecorder()
	s.handleDeleteSource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id, got %d", w.Code)
	}
}

// ---------- handleAdapterLookupError switch branches ----------

func TestHandleAdapterLookupError_AWSCase(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: aws", errInvalidSourceType)
	handleAdapterLookupError(w, err, "src-1", "aws")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAdapterLookupError_K8sCase(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: k8s", errInvalidSourceType)
	handleAdapterLookupError(w, err, "src-1", "k8s")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAdapterLookupError_GitCase(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: git", errInvalidSourceType)
	handleAdapterLookupError(w, err, "src-1", "git")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAdapterLookupError_UnexpectedError(t *testing.T) {
	w := httptest.NewRecorder()
	handleAdapterLookupError(w, errors.New("unexpected internal error"), "src-1", "other")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ---------- handleGetNode error paths ----------

func TestHandleGetNode_EmptyID(t *testing.T) {
	s := New(&core.Services{Config: &config.Config{}, Adapters: adapters.NewRegistry()})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("GET", "/api/v1/graph/node/", nil)
	// PathValue("id") returns "" when not set
	w := httptest.NewRecorder()
	h.handleGetNode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty node id, got %d", w.Code)
	}
}

func TestHandleGetNode_GraphError(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	graphStore := graph.NewSQLiteStore(sqlStore.DB(), nil)
	sqlStore.Close() // close DB → GetNode fails

	s := New(&core.Services{Config: &config.Config{}, Graph: graphStore, Adapters: adapters.NewRegistry()})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("GET", "/api/v1/graph/node/some-id", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	h.handleGetNode(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed DB, got %d", w.Code)
	}
}
