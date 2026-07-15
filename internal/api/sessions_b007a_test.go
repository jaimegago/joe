package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestB007a_PerUserSoftDeleteRestoreAudited proves the per-user owner lifecycle
// transitions are AUDITED in their effect transaction (§12.5): an owner DELETE is
// a soft-delete (trash) — the session is NOT physically gone, trashed_at/trashed_by
// are set, and exactly one session.trash audit row is written; restore clears the
// columns and writes exactly one session.restore row.
func TestB007a_PerUserSoftDeleteRestoreAudited(t *testing.T) {
	const alice = "user:alice@example.com"
	ts, sessRepo, _, st := newAdminSessionsServer(t)
	sid := createDefaultSession(t, sessRepo, alice)

	// Owner soft-delete → 204, trashed (not gone), one session.trash audit row.
	r := doRequest(t, http.MethodDelete, ts.URL+"/api/v1/sessions/"+sid, alice, nil)
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("owner soft-delete = %d, want 204", r.StatusCode)
	}
	r.Body.Close()
	sess, _ := sessRepo.GetSession(context.Background(), sid)
	if sess == nil || sess.TrashedAt == nil || sess.TrashedBy == nil || *sess.TrashedBy != alice {
		t.Fatalf("after soft-delete: want trashed with trashed_by=alice, got %+v", sess)
	}
	if n := countAudit(t, st, audit.ActionSessionTrash); n != 1 {
		t.Errorf("session.trash audit rows = %d, want 1", n)
	}

	// Owner restore → 200, active again, one session.restore audit row.
	rr := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+sid+"/restore", alice, nil)
	if rr.StatusCode != http.StatusOK {
		t.Fatalf("owner restore = %d, want 200", rr.StatusCode)
	}
	rr.Body.Close()
	sess, _ = sessRepo.GetSession(context.Background(), sid)
	if sess == nil || sess.TrashedAt != nil || sess.PurgeAfter != nil {
		t.Errorf("after restore: lifecycle columns not cleared: %+v", sess)
	}
	if n := countAudit(t, st, audit.ActionSessionRestore); n != 1 {
		t.Errorf("session.restore audit rows = %d, want 1", n)
	}

	// A non-owner cannot soft-delete (404), writing no audit row.
	const bob = "user:bob@example.com"
	rb := doRequest(t, http.MethodDelete, ts.URL+"/api/v1/sessions/"+sid, bob, nil)
	if rb.StatusCode != http.StatusNotFound {
		t.Errorf("non-owner soft-delete = %d, want 404", rb.StatusCode)
	}
	rb.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionTrash); n != 1 {
		t.Errorf("non-owner soft-delete wrote a trash audit row; total = %d, want 1", n)
	}
}

// TestB007a_PerUserSoftDeleteAuditFailureRollsBack proves the per-user soft-delete
// has TRUE same-tx effect↔audit coupling: a forced audit-insert failure rolls back
// the trashed_at write, so the session stays ACTIVE (not trashed) and the request
// fails 500 rather than silently trashing without a trail.
func TestB007a_PerUserSoftDeleteAuditFailureRollsBack(t *testing.T) {
	const alice = "user:alice@example.com"
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	svc := &core.Services{
		Store: s, SessionModel: sessRepo, RBACEnabled: false,
		Audit: failingAuditRepo{},
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(rbac.IdentityMiddleware(testPrincipalProvider{})(mux))
	t.Cleanup(ts.Close)

	sid := createDefaultSession(t, sessRepo, alice)
	r := doRequest(t, http.MethodDelete, ts.URL+"/api/v1/sessions/"+sid, alice, nil)
	if r.StatusCode != http.StatusInternalServerError {
		t.Errorf("soft-delete with failing audit = %d, want 500", r.StatusCode)
	}
	r.Body.Close()

	// Rollback: the session is still ACTIVE (trashed_at not set).
	sess, _ := sessRepo.GetSession(context.Background(), sid)
	if sess == nil {
		t.Fatal("session physically removed by a rolled-back soft-delete")
	}
	if sess.TrashedAt != nil {
		t.Error("trashed_at set despite the audit failure — soft-delete effect↔audit is not same-tx")
	}
}
