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
	"github.com/jaimegago/joe/internal/sessionarchive"
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
		Store:          s,
		SessionModel:   sessRepo,
		SessionArchive: sessionarchive.New(sessionarchive.NewFilesystemProvider(t.TempDir()), sessRepo),
		RBAC:           rbacRepo,
		RBACEnabled:    true,
		Audit:          auditRepo,
	}
	// Injected engine (rbac-engine-split); this admin-sessions stack also serves
	// the regime promote route, so pass the bare engine the handler used to build.
	srv := api.New(svc, rbac.NewPolicyEngine(rbacRepo))
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
// actions (cross-tenant), with RBAC enabled and a genuine admin capability. After
// B007a the effects split:
//   - purge: manifest-with-hard-stop (no-confirm preview is 200 + no audit +
//     session survives; confirmed is 200 + 1 audit + session gone);
//   - configure_retention: a real policy write (200 + 1 audit);
//   - archive / restore-archive: real provider-backed effects (B007c) — archive
//     200 + 1 audit, restore 200 + 1 audit, each coupled to its effect.
//
// Cross-tenant reads write no audit.
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

	// Archive (B007c): real provider-backed effect cross-tenant → 200 + exactly 1
	// session.archive audit row; the session is now archived.
	ra := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/archive", b006AdminPrincipal, nil)
	if ra.StatusCode != http.StatusOK {
		t.Errorf("admin archive = %d, want 200 (real provider effect)", ra.StatusCode)
	}
	ra.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionArchive); n != 1 {
		t.Errorf("archive audit rows = %d, want exactly 1", n)
	}
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess == nil || sess.ArchivedAt == nil {
		t.Fatalf("session not archived after admin archive: %+v", sess)
	}

	// Restore-archive (B007c): rehydrate → 200 + exactly 1 session.unarchive audit
	// row; the session is active again.
	ru := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/restore-archive", b006AdminPrincipal, nil)
	if ru.StatusCode != http.StatusOK {
		t.Errorf("admin restore-archive = %d, want 200 (real provider effect)", ru.StatusCode)
	}
	ru.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionUnarchive); n != 1 {
		t.Errorf("unarchive audit rows = %d, want exactly 1", n)
	}
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess == nil || sess.ArchivedAt != nil {
		t.Fatalf("session still archived after restore: %+v", sess)
	}

	// configure_retention (policy-scoped PUT): real effect now → 200 + 1 audit.
	r := doRequest(t, http.MethodPut, ts.URL+"/api/v1/admin/sessions/retention-policy", b006AdminPrincipal,
		map[string]any{"inactivity_window": "off", "trash_grace_days": 30, "terminal_action": "trash_then_purge"})
	if r.StatusCode != http.StatusOK {
		t.Errorf("admin PUT retention-policy = %d, want 200 (real effect)", r.StatusCode)
	}
	r.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionConfigureRetention); n != 1 {
		t.Errorf("audit rows for configure_retention = %d, want exactly 1", n)
	}

	// purge preview (no confirm): manifest hard stop → 200, NO audit, session
	// survives.
	rp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", b006AdminPrincipal, nil)
	if rp.StatusCode != http.StatusOK {
		t.Errorf("purge preview = %d, want 200 (manifest hard stop)", rp.StatusCode)
	}
	rp.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionPurge); n != 0 {
		t.Errorf("purge preview wrote %d audit rows, want 0 (nothing governed yet)", n)
	}
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess == nil {
		t.Fatal("purge preview destroyed the session (must be a hard stop)")
	}

	// purge confirmed: expunge → 200, exactly 1 audit row, session gone.
	rc := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", b006AdminPrincipal,
		map[string]any{"confirm": true})
	if rc.StatusCode != http.StatusOK {
		t.Errorf("purge confirmed = %d, want 200 (expunge)", rc.StatusCode)
	}
	rc.Body.Close()
	if n := countAudit(t, st, audit.ActionSessionPurge); n != 1 {
		t.Errorf("purge confirmed audit rows = %d, want exactly 1", n)
	}
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess != nil {
		t.Fatal("session survived a confirmed purge")
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
//   - PERMITTED the govern action through the ADMIN route (200 manifest).
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

	// ADMIN route: the SAME admin govern action on the SAME session → 200 manifest
	// (admin relationship resolved; the no-confirm purge returns the hard-stop
	// manifest rather than 501). The opposite outcome from the per-user 404 above
	// is what proves the admin relationship resolves only behind the admin prefix.
	if r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", b006AdminPrincipal, nil); r.StatusCode != http.StatusOK {
		t.Errorf("admin admin-route purge = %d, want 200 (admin relationship resolved, manifest hard stop)", r.StatusCode)
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

// TestB006_GovernAuditFailureFailsClosed proves the SAME-TX effect↔audit
// coupling B007a upgraded from B006's decision↔audit coupling: a CONFIRMED purge
// runs its expunge and its audit row in one transaction (mutateWithAudit), so a
// forced audit-insert failure returns 500, does NOT report success, AND rolls the
// expunge back — the session survives. (B006 could only prove the decision was
// not reported without its row, because the effect was deferred; here the effect
// itself is rolled back.)
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
	srv := api.New(svc, rbac.NewPolicyEngine(rbacRepo))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(rbac.IdentityMiddleware(testPrincipalProvider{})(mux))
	t.Cleanup(ts.Close)

	sid := createDefaultSession(t, sessRepo, alice)
	// Confirmed purge: reaches the audited effect. The failing audit insert rolls
	// the whole transaction back.
	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/purge", b006AdminPrincipal,
		map[string]any{"confirm": true})
	if r.StatusCode != http.StatusInternalServerError {
		t.Errorf("govern with failing audit = %d, want 500 (fail-closed, no silent success)", r.StatusCode)
	}
	r.Body.Close()
	// Same-tx rollback: the expunge was rolled back with the failed audit row, so
	// the session is still present.
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess == nil {
		t.Error("session was purged despite the audit failure — effect↔audit coupling is not same-tx")
	}
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
	srv := api.New(svc, nil)                                                    // RBAC disabled → nil engine (admin routes gate via the seam, not the engine)
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

// TestB006_DeferredReadRoutesReportPending proves the read-class surfaces that
// B006 deferred (all-trash list, retention-policy get) now return REAL data in
// B007a. The archive / restore-archive govern routes — once the only genuinely
// deferred surfaces — now return REAL provider-backed effects in B007c (200), not
// 501 pending; restore on a not-archived session is a 409, never a fabricated
// success.
func TestB006_DeferredReadRoutesReportPending(t *testing.T) {
	const alice = "user:alice@example.com"
	ts, sessRepo, _, _ := newAdminSessionsServer(t)
	sid := createDefaultSession(t, sessRepo, alice)

	// B007a-backed reads: real data, 200.
	for _, path := range []string{
		"/api/v1/admin/sessions/trash",
		"/api/v1/admin/sessions/retention-policy",
	} {
		r := doRequest(t, http.MethodGet, ts.URL+path, b006AdminPrincipal, nil)
		if r.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (B007a store)", path, r.StatusCode)
		}
		r.Body.Close()
	}

	// Restore on a not-yet-archived session: 409 (honest refusal), not 501, not a
	// fabricated success.
	rn := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/restore-archive", b006AdminPrincipal, nil)
	if rn.StatusCode != http.StatusConflict {
		t.Errorf("restore not-archived = %d, want 409", rn.StatusCode)
	}
	rn.Body.Close()

	// Archive then restore: both real effects, 200.
	ra := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/archive", b006AdminPrincipal, nil)
	if ra.StatusCode != http.StatusOK {
		t.Errorf("archive = %d, want 200 (B007c provider effect)", ra.StatusCode)
	}
	ra.Body.Close()
	rr := doRequest(t, http.MethodPost, ts.URL+"/api/v1/admin/sessions/"+sid+"/restore-archive", b006AdminPrincipal, nil)
	if rr.StatusCode != http.StatusOK {
		t.Errorf("restore-archive = %d, want 200 (B007c provider effect)", rr.StatusCode)
	}
	rr.Body.Close()
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
// actions that are NOT lifecycle transitions (rename, link) are NOT audited — §12
// audits admin + sweeper + lifecycle transitions only, and rename/link are none of
// those. (Owner soft-delete and restore ARE audited lifecycle transitions as of
// B007a — proven separately in the lifecycle tests; this test deliberately exercises
// only rename.) This guards against accidentally over-auditing the per-user surface.
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
