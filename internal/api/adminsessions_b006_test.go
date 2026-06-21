package api_test

import (
	"context"
	"database/sql"
	"errors"
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

const b006AdminPrincipal = "user:admin@example.com"

// newAdminSessionsServer wires the full session stack with RBAC ENABLED, a
// genuine dynamic admin (b006AdminPrincipal via AddAdmin), and a real append-only
// audit repository so B006 govern audit rows are observable. It returns the test
// server, the session repo, the rbac repo, and the store (for audit-row counts).
func newAdminSessionsServer(t *testing.T) (*httptest.Server, sessionmodel.Repository, rbac.Repository, *store.Store) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	rbacRepo := rbac.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	if err := rbacRepo.AddAdmin(context.Background(), rbac.Admin{
		Principal: b006AdminPrincipal, GrantedBy: "test", Reason: "b006 admin namespace test",
	}, "test"); err != nil {
		t.Fatalf("AddAdmin: %v", err)
	}

	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RBAC:         rbacRepo,
		RBACEnabled:  true,
		Audit:        auditRepo,
	}
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessRepo, rbacRepo, s
}

func countAudit(t *testing.T, s *store.Store, action string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM audit_log WHERE action = ?", action).Scan(&n); err != nil {
		t.Fatalf("count audit %q: %v", action, err)
	}
	return n
}

// TestB006_AdminGovernCrossTenant proves an admin acting through
// /api/v1/admin/sessions on a session they do NOT own is PERMITTED the govern
// actions (cross-tenant), with RBAC enabled and a genuine admin capability. The
// store effects are deferred to B007, so the authorized+audited decision reports
// 501 pending — never a silent success. Each govern action writes exactly one
// append-only audit row; the cross-tenant reads write none.
func TestB006_AdminGovernCrossTenant(t *testing.T) {
	const alice = "user:alice@example.com"
	ts, sessRepo, _, st := newAdminSessionsServer(t)
	sid := createDefaultSession(t, sessRepo, alice) // owned by alice, not the admin

	// Cross-tenant READ: admin can list/get/get-messages another principal's
	// session (ordinary read, no audit).
	for _, path := range []string{
		"/api/v1/admin/sessions",
		"/api/v1/admin/sessions/" + sid,
		"/api/v1/admin/sessions/" + sid + "/messages",
	} {
		r := doRequest(t, http.MethodGet, ts.URL+path, b006AdminPrincipal, nil)
		if r.StatusCode != http.StatusOK {
			t.Errorf("admin GET %s = %d, want 200 (cross-tenant read)", path, r.StatusCode)
		}
		r.Body.Close()
	}

	// GOVERN actions (effect deferred): authorized + audited → 501 pending.
	govern := []struct {
		path   string
		action string
	}{
		{"/api/v1/admin/sessions/" + sid + "/purge", audit.ActionSessionPurge},
		{"/api/v1/admin/sessions/" + sid + "/archive", audit.ActionSessionArchive},
		{"/api/v1/admin/sessions/" + sid + "/restore-archive", audit.ActionSessionUnarchive},
	}
	for _, g := range govern {
		r := doRequest(t, http.MethodPost, ts.URL+g.path, b006AdminPrincipal, nil)
		if r.StatusCode != http.StatusNotImplemented {
			t.Errorf("admin POST %s = %d, want 501 (authorized, effect pending B007)", g.path, r.StatusCode)
		}
		r.Body.Close()
		if n := countAudit(t, st, g.action); n != 1 {
			t.Errorf("audit rows for %s = %d, want exactly 1 (one append-only govern row)", g.action, n)
		}
	}

	// configure_retention (policy-scoped PUT): authorized + audited → 501.
	r := doRequest(t, http.MethodPut, ts.URL+"/api/v1/admin/sessions/retention-policy", b006AdminPrincipal,
		map[string]any{"inactivity_window": "off", "trash_grace_days": 30, "terminal_action": "trash_then_purge"})
	if r.StatusCode != http.StatusNotImplemented {
		t.Errorf("admin PUT retention-policy = %d, want 501 (authorized, effect pending B007)", r.StatusCode)
	}
	r.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionConfigureRetention); n != 1 {
		t.Errorf("audit rows for configure_retention = %d, want exactly 1", n)
	}

	// The session survives every (effect-deferred) govern call.
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess == nil {
		t.Fatal("session destroyed by an effect-deferred govern action")
	}
}

// TestB006_NonAdminDeniedOnAdminNamespace proves a non-admin acting through
// /api/v1/admin/sessions is DENIED (403) by the requireAdmin prefix gate — even
// on their OWN session — and that the denial writes no govern audit row.
func TestB006_NonAdminDeniedOnAdminNamespace(t *testing.T) {
	const alice = "user:alice@example.com"
	ts, sessRepo, _, st := newAdminSessionsServer(t)
	sid := createDefaultSession(t, sessRepo, alice) // alice owns it

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/sessions"},
		{http.MethodGet, "/api/v1/admin/sessions/" + sid},
		{http.MethodPost, "/api/v1/admin/sessions/" + sid + "/purge"},
		{http.MethodPost, "/api/v1/admin/sessions/" + sid + "/archive"},
		{http.MethodPut, "/api/v1/admin/sessions/retention-policy"},
	} {
		r := doRequest(t, tc.method, ts.URL+tc.path, alice, nil) // alice is NOT admin
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("non-admin %s %s = %d, want 403", tc.method, tc.path, r.StatusCode)
		}
		r.Body.Close()
	}

	// No govern audit rows were written by the denied non-admin (purge/archive).
	if n := countAudit(t, st, audit.ActionSessionPurge); n != 0 {
		t.Errorf("purge audit rows after non-admin denial = %d, want 0", n)
	}
	if n := countAudit(t, st, audit.ActionSessionArchive); n != 0 {
		t.Errorf("archive audit rows after non-admin denial = %d, want 0", n)
	}
}

// TestB006_AdminRelationshipUnreachableFromPerUser is the structural cross-tenant
// proof: the admin RELATIONSHIP is reachable ONLY through the admin prefix's seam
// instance, never via a per-user route. With RBAC enabled and the caller holding
// the genuine dynamic admin capability, the SAME admin principal is:
//   - DENIED owner-mutate on a non-owned session through a PER-USER route (404 —
//     the B005 always-false suppression still holds), yet
//   - PERMITTED the govern action through the ADMIN route (501 pending).
//
// Same principal, same target session, opposite outcomes by route prefix — which
// is exactly the §12.8 "admin relationship only behind the admin prefix" model.
func TestB006_AdminRelationshipUnreachableFromPerUser(t *testing.T) {
	const alice = "user:alice@example.com"
	ts, sessRepo, rbacRepo, _ := newAdminSessionsServer(t)

	if ok, err := rbacRepo.IsAdmin(context.Background(), b006AdminPrincipal); err != nil || !ok {
		t.Fatalf("IsAdmin(admin) = %v,%v; want true,nil (a real admin capability must be live)", ok, err)
	}
	sid := createDefaultSession(t, sessRepo, alice)

	// PER-USER route: admin owner-mutate (rename) on alice's session → 404
	// (admin relationship suppressed on the per-user seam instance).
	if r := doRequest(t, http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, b006AdminPrincipal,
		map[string]any{"title": "admin hijack"}); r.StatusCode != http.StatusNotFound {
		t.Errorf("admin per-user rename = %d, want 404 (admin suppressed on per-user route)", r.StatusCode)
		r.Body.Close()
	} else {
		r.Body.Close()
	}

	// ADMIN route: the SAME admin govern action on the SAME session → 501
	// (admin relationship resolved, decision authorized, effect deferred).
	if r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", b006AdminPrincipal, nil); r.StatusCode != http.StatusNotImplemented {
		t.Errorf("admin admin-route purge = %d, want 501 (admin relationship resolved)", r.StatusCode)
		r.Body.Close()
	} else {
		r.Body.Close()
	}
}

// failingAuditRepo is an audit.Repository whose Insert always fails — used to
// prove the govern routes fail CLOSED: an authorized decision that cannot be
// recorded is aborted (500), never reported as a completed governance action.
type failingAuditRepo struct{}

func (failingAuditRepo) Insert(context.Context, audit.Event) error {
	return errors.New("forced audit failure")
}
func (failingAuditRepo) InsertTx(context.Context, *sql.Tx, audit.Event) error {
	return errors.New("forced audit failure")
}

// TestB006_GovernAuditFailureFailsClosed proves the decision↔audit coupling: when
// the audit insert fails, the govern route returns 500 and does NOT report the
// 501 pending success. (In B006 every govern effect is deferred to B007, so there
// is no store mutation to roll back yet; the coupling proven here is that the
// authorized DECISION cannot be reported without its durable row. When B007 adds
// the real transitions, the same row moves into the effect's transaction via
// mutateWithAudit, upgrading this to same-tx rollback.)
func TestB006_GovernAuditFailureFailsClosed(t *testing.T) {
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
	rbacRepo := rbac.NewRepository(s.DB(), store.DriverSQLite)
	if err := rbacRepo.AddAdmin(context.Background(), rbac.Admin{
		Principal: b006AdminPrincipal, GrantedBy: "test", Reason: "fail-closed test",
	}, "test"); err != nil {
		t.Fatalf("AddAdmin: %v", err)
	}
	svc := &core.Services{
		Store: s, SessionModel: sessRepo, RBAC: rbacRepo, RBACEnabled: true,
		Audit: failingAuditRepo{},
	}
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(rbac.IdentityMiddleware(testPrincipalProvider{})(mux))
	t.Cleanup(ts.Close)

	sid := createDefaultSession(t, sessRepo, alice)
	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", b006AdminPrincipal, nil)
	if r.StatusCode != http.StatusInternalServerError {
		t.Errorf("govern with failing audit = %d, want 500 (fail-closed, no silent success)", r.StatusCode)
	}
	r.Body.Close()
}

// TestB006_RBACDisabledAsymmetry documents the deliberate gate/seam asymmetry: with
// RBAC DISABLED the requireAdmin prefix gate PERMITS (auth-disabled convention),
// but the admin SEAM (real D-0011 checker) resolves NO admin, so the BOTH-conditions
// govern routes still DENY (403). The safe intersection holds: cross-tenant
// governance cannot fire without a real admin even when the gate is open.
func TestB006_RBACDisabledAsymmetry(t *testing.T) {
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
	svc := &core.Services{Store: s, SessionModel: sessRepo, RBACEnabled: false} // RBAC OFF
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(rbac.IdentityMiddleware(testPrincipalProvider{})(mux))
	t.Cleanup(ts.Close)

	sid := createDefaultSession(t, sessRepo, alice)

	// Read: requireAdmin permits under RBAC-off → 200 (reads need only the gate).
	rr := doRequest(t, http.MethodGet, ts.URL+"/api/v1/admin/sessions/"+sid, alice, nil)
	if rr.StatusCode != http.StatusOK {
		t.Errorf("RBAC-off admin read = %d, want 200 (gate permits)", rr.StatusCode)
	}
	rr.Body.Close()

	// Govern: gate permits but the admin seam denies (no real admin) → 403.
	rg := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", alice, nil)
	if rg.StatusCode != http.StatusForbidden {
		t.Errorf("RBAC-off admin govern = %d, want 403 (seam denies — no real admin)", rg.StatusCode)
	}
	rg.Body.Close()
}

// TestB006_DeferredReadRoutesReportPending proves the read-class deferred surfaces
// (all-trash list, retention-policy get) return 501 pending rather than a 200 with
// fabricated empty data — their backing stores land in B007.
func TestB006_DeferredReadRoutesReportPending(t *testing.T) {
	ts, _, _, _ := newAdminSessionsServer(t)
	for _, path := range []string{
		"/api/v1/admin/sessions/trash",
		"/api/v1/admin/sessions/retention-policy",
	} {
		r := doRequest(t, http.MethodGet, ts.URL+path, b006AdminPrincipal, nil)
		if r.StatusCode != http.StatusNotImplemented {
			t.Errorf("GET %s = %d, want 501 (store deferred to B007)", path, r.StatusCode)
		}
		r.Body.Close()
	}
}

// TestB006_DualDeclareSingleBackendSurface documents the dual-declare
// disposition: §12 specifies ONE promote-in-place transition reached by TWO UI
// entry points, satisfied by a SINGLE backend surface. Both transports —
// `/regime/declare` (control-plane / CLI alias, body session_id) and
// `/sessions/{id}/promote-incident` (canonical per-user route, path id) — reach
// the IDENTICAL backend: the same DeclareIncidentRegime promote-in-place
// transition, gated by the SAME regime-control zone (NOT the session seam). The
// shared regime-control authz is proven structurally: a caller WITHOUT the
// regime-control grant is 403 on BOTH (the session seam would have allowed the
// owner — so neither route runs through it). Neither route was removed: the
// alias is retained for the reused control plane + CLI (§12.10).
func TestB006_DualDeclareSingleBackendSurface(t *testing.T) {
	ts, sessRepo, rbacRepo, _ := newAdminSessionsServer(t)
	const alice = "user:alice@example.com"

	// Same regime-control authz on BOTH transports: the owner (who the session
	// seam would allow to mutate) is 403 on both until granted regime-control.
	s1 := createDefaultSession(t, sessRepo, alice)
	for _, path := range []string{
		"/api/v1/regime/declare",
		"/api/v1/sessions/" + s1 + "/promote-incident",
	} {
		body := map[string]any(nil)
		if path == "/api/v1/regime/declare" {
			body = map[string]any{"session_id": s1}
		}
		r := doRequest(t, http.MethodPost, ts.URL+path, alice, body)
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("ungranted owner on %s = %d, want 403 (regime-control zone, not session seam)", path, r.StatusCode)
		}
		r.Body.Close()
	}

	grantRegimeControl(t, rbacRepo, alice)

	// Alias transport: /regime/declare (body id) reaches the promote-in-place
	// transition → 201, same session promoted.
	sA := createDefaultSession(t, sessRepo, alice)
	rA := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", alice, map[string]any{"session_id": sA})
	if rA.StatusCode != http.StatusCreated {
		t.Fatalf("/regime/declare (alias) = %d, want 201", rA.StatusCode)
	}
	rA.Body.Close()
	if sess, _ := sessRepo.GetSession(context.Background(), sA); sess == nil || sess.Type != sessionmodel.SessionTypeIncident {
		t.Errorf("alias transport did not promote-in-place: %+v", sess)
	}
	// The CANONICAL transport's promote-in-place (path id → same
	// DeclareIncidentRegime transition) is proven by
	// TestB005_PromoteIncidentPerUserRoute; this test pins that BOTH transports
	// share the one regime-control-gated backend surface.
}

// TestB006_PerUserMutatesUnaudited pins the §12 audit scope: per-user owner-mutate
// actions (rename, link) are NOT audited — §12 audits admin + sweeper + lifecycle
// transitions only, and rename/link are none of those. (Owner soft-delete becomes
// an audited trash transition in B007 when the soft-delete EFFECT lands; the
// current per-user DELETE is a hard-delete placeholder and is likewise unaudited
// here.) This guards against accidentally over-auditing the per-user surface.
func TestB006_PerUserMutatesUnaudited(t *testing.T) {
	const alice = "user:alice@example.com"
	ts, sessRepo, _, st := newAdminSessionsServer(t)
	sid := createDefaultSession(t, sessRepo, alice)

	// Owner rename (per-user write) — succeeds, writes no audit row.
	r := doRequest(t, http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, alice,
		map[string]any{"title": "renamed"})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("owner rename = %d, want 200", r.StatusCode)
	}
	r.Body.Close()

	// No session-governance audit verb was written by a per-user mutate.
	for _, action := range []string{
		audit.ActionSessionPurge, audit.ActionSessionArchive,
		audit.ActionSessionUnarchive, audit.ActionSessionConfigureRetention,
	} {
		if n := countAudit(t, st, action); n != 0 {
			t.Errorf("per-user rename wrote %d %q rows, want 0 (per-user mutates are unaudited)", n, action)
		}
	}
}
