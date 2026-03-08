package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
)

// openRBACTestDB creates an in-memory SQLite DB with the RBAC schema seeded.
func openRBACTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE sources (id TEXT PRIMARY KEY, name TEXT);
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
		INSERT INTO security_zones VALUES ('prod-readonly','Production Read-Only','','["read","query"]','2026-01-01T00:00:00Z');
		INSERT INTO security_zones VALUES ('unassigned','Unassigned','','["read"]','2026-01-01T00:00:00Z');
		INSERT INTO sources VALUES ('src-1','test source');
	`)
	if err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	return db
}

func newAdminServer(t *testing.T) (*httptest.Server, rbac.Repository) {
	t.Helper()
	db := openRBACTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")

	svc := &core.Services{RBAC: repo}
	srv := api.New(svc)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, repo
}

func TestAdminListZones(t *testing.T) {
	ts, _ := newAdminServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/admin/zones")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	count, _ := result["count"].(float64)
	if count < 2 {
		t.Errorf("expected at least 2 seeded zones, got %v", count)
	}
}

func TestAdminCreateZone(t *testing.T) {
	ts, _ := newAdminServer(t)

	body, _ := json.Marshal(map[string]any{
		"id":              "staging",
		"name":            "Staging",
		"description":     "Staging environment",
		"allowed_actions": []string{"read", "query", "mutate"},
	})

	resp, err := http.Post(ts.URL+"/api/v1/admin/zones", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestAdminCreateZone_MissingID(t *testing.T) {
	ts, _ := newAdminServer(t)

	body, _ := json.Marshal(map[string]any{"name": "No ID zone"})
	resp, err := http.Post(ts.URL+"/api/v1/admin/zones", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing id, got %d", resp.StatusCode)
	}
}

func TestAdminListAssignments(t *testing.T) {
	ts, repo := newAdminServer(t)

	// Add one assignment first.
	_ = repo.UpsertAssignment(context.Background(), rbac.SourceZoneAssignment{
		SourceID: "src-1", ZoneID: "prod-readonly", AssignedBy: "test",
	})

	resp, err := http.Get(ts.URL + "/api/v1/admin/source-zones")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	count, _ := result["count"].(float64)
	if count != 1 {
		t.Errorf("expected 1 assignment, got %v", count)
	}
}

func TestAdminAssignSourceZone(t *testing.T) {
	ts, _ := newAdminServer(t)

	body, _ := json.Marshal(map[string]any{
		"source_id":   "src-1",
		"zone_id":     "prod-readonly",
		"assigned_by": "admin",
		"reason":      "initial assignment",
	})

	resp, err := http.Post(ts.URL+"/api/v1/admin/source-zones", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminListPolicies(t *testing.T) {
	ts, repo := newAdminServer(t)
	_, _ = repo.CreatePolicy(context.Background(), rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"})

	resp, err := http.Get(ts.URL + "/api/v1/admin/policies")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	count, _ := result["count"].(float64)
	if count != 1 {
		t.Errorf("expected 1 policy, got %v", count)
	}
}

func TestAdminCreatePolicy(t *testing.T) {
	ts, _ := newAdminServer(t)

	body, _ := json.Marshal(map[string]any{
		"principal": "bob",
		"zone_id":   "unassigned",
	})

	resp, err := http.Post(ts.URL+"/api/v1/admin/policies", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestAdminDeletePolicy(t *testing.T) {
	ts, repo := newAdminServer(t)
	p, _ := repo.CreatePolicy(context.Background(), rbac.Policy{Principal: "eve", ZoneID: "unassigned"})

	req, _ := http.NewRequest("DELETE",
		ts.URL+"/api/v1/admin/policies/"+itoa(p.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminListUnassigned(t *testing.T) {
	ts, _ := newAdminServer(t)
	// src-1 has no zone assignment yet.

	resp, err := http.Get(ts.URL + "/api/v1/admin/unassigned")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	count, _ := result["count"].(float64)
	if count != 1 {
		t.Errorf("expected 1 unassigned source, got %v", count)
	}
}

func itoa(i int64) string {
	return string(rune('0' + i)) // works for single-digit IDs in tests
}
