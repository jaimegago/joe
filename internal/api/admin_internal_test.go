package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/rbac"
)

// openClosedRBACDB creates an in-memory SQLite DB with minimal RBAC schema,
// then closes it so that every subsequent query returns "sql: database is closed".
func openClosedRBACDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE security_zones (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT,
			allowed_actions TEXT NOT NULL DEFAULT '["read"]', created_at TEXT NOT NULL
		);
		CREATE TABLE source_zone_assignments (
			source_id TEXT NOT NULL, zone_id TEXT NOT NULL, assigned_by TEXT NOT NULL,
			reason TEXT, assigned_at TEXT NOT NULL, PRIMARY KEY (source_id)
		);
		CREATE TABLE rbac_policies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, principal TEXT NOT NULL,
			zone_id TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE (principal, zone_id)
		);
		CREATE TABLE sources (id TEXT PRIMARY KEY, name TEXT);
	`); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	db.Close() // closed DB → all subsequent queries fail
	return db
}

func newClosedDBAdminHandler(t *testing.T) *adminHandler {
	t.Helper()
	db := openClosedRBACDB(t)
	return &adminHandler{repo: rbac.NewRepository(db, "sqlite")}
}

func TestAdminListZones_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/zones", nil)
	w := httptest.NewRecorder()
	h.listZones(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminListAssignments_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/source-zones", nil)
	w := httptest.NewRecorder()
	h.listAssignments(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminListPolicies_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/policies", nil)
	w := httptest.NewRecorder()
	h.listPolicies(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminListUnassigned_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/unassigned", nil)
	w := httptest.NewRecorder()
	h.listUnassigned(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminCreateZone_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	body := `{"id":"z1","name":"Zone One"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/zones", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.createZone(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminAssignSourceZone_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	body := `{"source_id":"s1","zone_id":"z1","assigned_by":"admin"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/source-zones", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.assignSourceZone(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminCreatePolicy_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	body := `{"principal":"alice","zone_id":"z1"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/policies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.createPolicy(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminDeletePolicy_RepoError(t *testing.T) {
	h := newClosedDBAdminHandler(t)
	req := httptest.NewRequest("DELETE", "/api/v1/admin/policies/1", nil)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	h.deletePolicy(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}
