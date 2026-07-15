package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

const b005AdminPrincipal = "user:admin@example.com"

// newPerUserAdminServer wires the per-user /api/v1/sessions surface with RBAC
// ENABLED and a genuine dynamic admin (b005AdminPrincipal granted via AddAdmin).
// It exists to prove the §12.8 two-instance defense-in-depth: even with a real
// admin capability present, the per-user seam instance (alwaysFalseAdminChecker)
// suppresses the admin relationship so an admin cannot owner-mutate a session it
// does not own through a per-user route.
func newPerUserAdminServer(t *testing.T) (*httptest.Server, sessionmodel.Repository, rbac.Repository) {
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
	if err := rbacRepo.AddAdmin(context.Background(), rbac.Admin{
		Principal: b005AdminPrincipal, GrantedBy: "test", Reason: "b005 suppression test",
	}, "test"); err != nil {
		t.Fatalf("AddAdmin: %v", err)
	}

	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RBAC:         rbacRepo,
		RBACEnabled:  true, // a real admin capability is live — the per-user seam must STILL suppress it
	}
	// promote-incident authorizes through the regime-control zone on the injected
	// engine (rbac-engine-split); pass the bare engine the handler used to build
	// so the admin-suppression assertions are unchanged.
	srv := api.New(svc, rbac.NewPolicyEngine(rbacRepo))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessRepo, rbacRepo
}

// TestB005_PerUserRouteSuppressesAdmin is the structural proof of the §12.8
// defense-in-depth decision: a per-user /api/v1/sessions mutation route can
// never resolve an admin relationship. With RBAC enabled and the caller holding
// the genuine dynamic admin capability, an owner-mutate on a session the admin
// does not own is DENIED (the admin resolves as team-member/non-owner), while
// read still succeeds (team-wide). The owner can mutate; a non-owner non-admin
// cannot.
func TestB005_PerUserRouteSuppressesAdmin(t *testing.T) {
	const alice, bob = "user:alice@example.com", "user:bob@example.com"
	ts, sessRepo, rbacRepo := newPerUserAdminServer(t)

	// Sanity: the admin principal genuinely holds the dynamic admin capability,
	// so a later deny is suppression — not the absence of admin.
	if ok, err := rbacRepo.IsAdmin(context.Background(), b005AdminPrincipal); err != nil || !ok {
		t.Fatalf("IsAdmin(admin) = %v,%v; want true,nil", ok, err)
	}

	sid := createDefaultSession(t, sessRepo, alice)

	// Admin owner-mutate (rename) on alice's session → DENIED 404 (suppressed).
	if r := doRequest(t, http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, b005AdminPrincipal,
		map[string]any{"title": "admin hijack"}); r.StatusCode != http.StatusNotFound {
		t.Errorf("admin per-user rename = %d, want 404 (admin relationship suppressed)", r.StatusCode)
		r.Body.Close()
	} else {
		r.Body.Close()
	}

	// Admin owner-mutate (delete) on alice's session → DENIED 404 (suppressed).
	if r := doRequest(t, http.MethodDelete, ts.URL+"/api/v1/sessions/"+sid, b005AdminPrincipal, nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("admin per-user delete = %d, want 404 (admin relationship suppressed)", r.StatusCode)
		r.Body.Close()
	} else {
		r.Body.Close()
	}

	// The session survives the suppressed admin mutations.
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess == nil {
		t.Fatal("session was deleted by a suppressed admin")
	}

	// Admin CAN read it (team-wide read), flagged read_only (non-owner).
	rg := doRequest(t, http.MethodGet, ts.URL+"/api/v1/sessions/"+sid, b005AdminPrincipal, nil)
	if rg.StatusCode != http.StatusOK {
		t.Errorf("admin read = %d, want 200 (team-wide read)", rg.StatusCode)
	}
	var sess map[string]any
	json.NewDecoder(rg.Body).Decode(&sess)
	rg.Body.Close()
	if sess["read_only"] != true {
		t.Errorf("admin read_only = %v, want true (admin reads as non-owner)", sess["read_only"])
	}

	// Owner CAN mutate; a non-owner non-admin CANNOT.
	if w := doRequest(t, http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, alice,
		map[string]any{"title": "owner ok"}); w.StatusCode != http.StatusOK {
		t.Errorf("owner rename = %d, want 200", w.StatusCode)
		w.Body.Close()
	} else {
		w.Body.Close()
	}
	if w := doRequest(t, http.MethodPatch, ts.URL+"/api/v1/sessions/"+sid, bob,
		map[string]any{"title": "bob hijack"}); w.StatusCode != http.StatusNotFound {
		t.Errorf("non-owner non-admin rename = %d, want 404", w.StatusCode)
		w.Body.Close()
	} else {
		w.Body.Close()
	}
}

// TestB005_LegacyAgentSessionsNamespaceGone proves the team-global
// /api/v1/agent-sessions namespace (CRUD + its findings sub-resource, including
// the unauthorized delete) is no longer registered, while the re-homed findings
// sub-resource IS served under /api/v1/sessions.
func TestB005_LegacyAgentSessionsNamespaceGone(t *testing.T) {
	ts, repo, _, _ := newSessionModelServer(t)
	if _, err := repo.CreateSession(t.Context(), sessionmodel.AgentSession{
		ID: "s1", Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "alice",
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	legacy := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/agent-sessions"},
		{http.MethodPost, "/api/v1/agent-sessions"},
		{http.MethodGet, "/api/v1/agent-sessions/s1"},
		{http.MethodDelete, "/api/v1/agent-sessions/s1"}, // the old unauthorized delete
		{http.MethodPost, "/api/v1/agent-sessions/s1/findings"},
		{http.MethodGet, "/api/v1/agent-sessions/s1/findings"},
	}
	for _, tc := range legacy {
		r := doRequest(t, tc.method, ts.URL+tc.path, "alice", nil)
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 (legacy /agent-sessions namespace removed)", tc.method, tc.path, r.StatusCode)
		}
		r.Body.Close()
	}

	// Positive: the re-homed findings sub-resource is served under /sessions.
	r := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/s1/findings", "alice",
		map[string]any{"source_session_id": "s1", "body": "x"})
	if r.StatusCode == http.StatusNotFound {
		t.Error("re-homed POST /sessions/{id}/findings returned 404 — sub-resource not served at new path")
	}
	r.Body.Close()
}

// TestB005_PromoteIncidentPerUserRoute proves the per-user promote-incident
// route (POST /api/v1/sessions/{id}/promote-incident, §12.8) is served and is
// authorized by the REGIME-CONTROL ZONE — NOT the session seam: a caller who
// OWNS the session but lacks the regime-control grant is still 403 (a
// seam-gated route would have allowed the owner). With the grant, it promotes
// the session in place (§12.3).
func TestB005_PromoteIncidentPerUserRoute(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)

	owned := createDefaultSession(t, sessRepo, "alice")

	// Owner without the regime-control grant → 403 (regime-control authz, not
	// the session seam, which would allow the owner).
	denied := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+owned+"/promote-incident", "alice", nil)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("ungranted owner promote = %d, want 403 (regime-control zone)", denied.StatusCode)
	}
	denied.Body.Close()

	// Unauthenticated → 401.
	un := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+owned+"/promote-incident", "", nil)
	if un.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated promote = %d, want 401", un.StatusCode)
	}
	un.Body.Close()

	// With the regime-control grant → 201, promote-in-place (same id).
	grantRegimeControl(t, rbacRepo, "alice")
	ok := doRequest(t, http.MethodPost, ts.URL+"/api/v1/sessions/"+owned+"/promote-incident", "alice", nil)
	if ok.StatusCode != http.StatusCreated {
		t.Fatalf("granted promote = %d, want 201", ok.StatusCode)
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(ok.Body).Decode(&body)
	ok.Body.Close()
	if body.SessionID != owned {
		t.Errorf("promote returned session_id = %q, want the promoted %q (promote-in-place)", body.SessionID, owned)
	}
	sess, _ := sessRepo.GetSession(context.Background(), owned)
	if sess == nil || sess.Type != sessionmodel.SessionTypeIncident {
		t.Errorf("promoted session = %+v, want type=incident", sess)
	}
}
