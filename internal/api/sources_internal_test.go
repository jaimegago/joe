package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/sessionmodel"
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

	s := New(&core.Services{Config: &config.Config{}, Graph: graphStore, Adapters: adapters.NewRegistry()})
	h := &graphHandler{server: s}
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

// closedSessionModel returns a session-model repository backed by a closed DB so
// every query fails with "sql: database is closed" — the substrate for the chat
// handlers' DB-error (500) paths now that they read the session model.
func closedSessionModel(t *testing.T) sessionmodel.Repository {
	t.Helper()
	return sessionmodel.NewRepository(newClosedStore(t).DB(), store.DriverSQLite)
}

// TestHandleCreateSession_StoreError covers the CreateSession error path.
func TestHandleCreateSession_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:       &config.Config{},
		SessionModel: closedSessionModel(t),
		Adapters:     adapters.NewRegistry(),
	})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("POST", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.handleCreateSession(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleListSessions_StoreError covers the ListSessionsByCreator error path.
func TestHandleListSessions_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:       &config.Config{},
		SessionModel: closedSessionModel(t),
		Adapters:     adapters.NewRegistry(),
	})
	h := &webUIHandler{server: s}
	req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.handleListSessions(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with closed store, got %d", w.Code)
	}
}

// TestHandleGetSessionMessages_StoreError covers the GetSession error path.
func TestHandleGetSessionMessages_StoreError(t *testing.T) {
	s := New(&core.Services{
		Config:       &config.Config{},
		SessionModel: closedSessionModel(t),
		Adapters:     adapters.NewRegistry(),
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

// ---------- handleAccessError switch branches ----------

func TestHandleAccessError_AWSWrongType(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: aws", access.ErrWrongAdapterType)
	if !handleAccessError(w, err, "src-1", "aws") {
		t.Fatal("expected handleAccessError to handle wrong-type error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAccessError_K8sWrongType(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: k8s", access.ErrWrongAdapterType)
	if !handleAccessError(w, err, "src-1", "k8s") {
		t.Fatal("expected handleAccessError to handle wrong-type error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAccessError_GitWrongType(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: git", access.ErrWrongAdapterType)
	if !handleAccessError(w, err, "src-1", "git") {
		t.Fatal("expected handleAccessError to handle wrong-type error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAccessError_PermissionDenied(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: principal", access.ErrPermissionDenied)
	if !handleAccessError(w, err, "src-1", "k8s") {
		t.Fatal("expected handleAccessError to handle permission-denied error")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestHandleAccessError_SourceNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: src-1", access.ErrSourceNotFound)
	if !handleAccessError(w, err, "src-1", "k8s") {
		t.Fatal("expected handleAccessError to handle source-not-found error")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleAccessError_UnexpectedError(t *testing.T) {
	w := httptest.NewRecorder()
	// A non-access error is not handled here; the caller writes it (500).
	if handleAccessError(w, errors.New("unexpected internal error"), "src-1", "other") {
		t.Fatal("expected handleAccessError to return false for a non-access error")
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
