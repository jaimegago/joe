package api_test

// End-to-end sign-on test for the Web UI auth surface (Streams H1/H1.5/H2/H3
// and the admin-grant follow-up). It is the automated counterpart to the manual
// browser walkthrough: it drives the REAL HTTP chain — auth.EdgeAuth +
// auth.Handlers + api.Server (/me, /auth/config) + the append-only audit log +
// server-side sessions — over a TLS httptest server with a cookie-jar client.
//
// The one thing it does NOT use is a live IdP: joe-core has no config seam to
// point the binary at a fake Google, and an automated test must not talk to the
// real one. So it injects a fakeProvider implementing the auth.Provider seam
// (the same seam the production oidcProvider implements). Everything else —
// state/nonce/CSRF handling, email_verified gating, principal derivation, admin
// bootstrap, session mint/revoke, and every audit write — is the real code path.
//
// A TLS server (not plain http) is required because the session and state
// cookies are Secure: Go's cookiejar will only store/send a Secure cookie over
// https, exactly as a browser would.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

const (
	e2eAdminEmail     = "gagojaime@gmail.com"
	e2eAdminPrincipal = "user:gagojaime@gmail.com"
	e2eBreakGlassName = "breakglass-test"
	e2eBreakGlassKey  = "bg-test-key-0123456789"
	e2eServerKey      = "server-test-key-0123456789"
)

// fakeProvider implements auth.Provider without a live IdP. It captures the
// nonce the login handler generates (handed to AuthCodeURL) and echoes it back
// from Verify, so the callback's nonce check passes — mirroring a real IdP
// round-tripping the nonce through the ID token.
type fakeProvider struct {
	claims auth.Claims
	nonce  string
}

func (f *fakeProvider) AuthCodeURL(_ context.Context, state, nonce, _ string) (string, error) {
	f.nonce = nonce
	return "https://idp.test/authorize?state=" + url.QueryEscape(state), nil
}

func (f *fakeProvider) Exchange(_ context.Context, _, _ string) (string, error) {
	return "raw-id-token", nil
}

func (f *fakeProvider) Verify(_ context.Context, _ string) (*auth.VerifiedToken, error) {
	return &auth.VerifiedToken{Claims: f.claims, Nonce: f.nonce}, nil
}

// authTestServer bundles a fully wired sign-on server for one test.
type authTestServer struct {
	ts   *httptest.Server
	db   *sql.DB
	prov *fakeProvider
}

// newAuthServer builds the real auth chain over a fresh in-memory SQLite DB and
// serves it via a TLS httptest server. adminEmail is the bootstrap admin; set it
// to "" to test the no-bootstrap path.
func newAuthServer(t *testing.T, adminEmail string) *authTestServer {
	t.Helper()

	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	rbacRepo := rbac.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	authRepo := auth.NewRepository(s.DB(), store.DriverSQLite)
	sessionMgr := auth.NewSessionManager(authRepo, time.Hour)

	prov := &fakeProvider{claims: auth.Claims{Email: e2eAdminEmail, EmailVerified: true}}
	handlers := auth.NewHandlers(auth.HandlerConfig{
		Provider:          prov,
		Sessions:          sessionMgr,
		Repo:              authRepo,
		RBAC:              rbacRepo,
		AdminEmail:        adminEmail,
		PostLoginRedirect: "/",
		Audit:             auditRepo,
	})

	saResolver, err := auth.NewServiceAccountResolver([]config.ServiceAccount{
		{Name: "server", Key: e2eServerKey},
		{Name: e2eBreakGlassName, Key: e2eBreakGlassKey},
	})
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}

	svc := &core.Services{
		Store:       s,
		RBAC:        rbacRepo,
		Audit:       auditRepo,
		RBACEnabled: true,
		OIDCEnabled: true,
	}
	srv := api.New(svc)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)                 // /api/v1/me, /api/v1/auth/config, /api/v1/status, ...
	handlers.RegisterRoutes(mux, "/api/v1") // /api/v1/auth/{login,callback,logout}

	// The real edge gate, configured exactly as cmd/joe-core wires it: OIDC on,
	// break-glass dedup window = the session TTL.
	handler := auth.EdgeAuth(auth.EdgeConfig{
		Sessions:         sessionMgr,
		ServiceAccounts:  saResolver,
		OIDCConfigured:   true,
		Audit:            auditRepo,
		AuditDedupWindow: time.Hour,
	})(mux)

	ts := httptest.NewTLSServer(handler)
	t.Cleanup(ts.Close)

	return &authTestServer{ts: ts, db: s.DB(), prov: prov}
}

// client returns a TLS client that trusts the test server, with a cookie jar
// and redirects disabled (we assert on 302s rather than following them).
func (a *authTestServer) client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	c := a.ts.Client()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// roundTripLogin performs the full OIDC sign-on: GET /auth/login (302 to IdP),
// then GET /auth/callback (302 to "/", sets the session cookie in the jar).
func (a *authTestServer) roundTripLogin(t *testing.T, c *http.Client) {
	t.Helper()

	loginResp, err := c.Get(a.ts.URL + "/api/v1/auth/login")
	if err != nil {
		t.Fatalf("GET /auth/login: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusFound {
		t.Fatalf("/auth/login status = %d, want 302", loginResp.StatusCode)
	}
	loc, err := url.Parse(loginResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("/auth/login Location carried no state")
	}

	cbResp, err := c.Get(a.ts.URL + "/api/v1/auth/callback?state=" + url.QueryEscape(state) + "&code=authcode")
	if err != nil {
		t.Fatalf("GET /auth/callback: %v", err)
	}
	defer cbResp.Body.Close()
	if cbResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("/auth/callback status = %d, want 302 (body=%s)", cbResp.StatusCode, body)
	}
	if loc := cbResp.Header.Get("Location"); loc != "/" {
		t.Fatalf("/auth/callback redirect = %q, want \"/\"", loc)
	}
}

// getJSON does GET path on c and decodes the JSON body into out, returning the
// status code.
func (a *authTestServer) getJSON(t *testing.T, c *http.Client, path string, out any) int {
	t.Helper()
	resp, err := c.Get(a.ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

// auditCount counts audit_log rows matching where (the audit repository exposes
// no list method by design, so the test reads the table directly).
func (a *authTestServer) auditCount(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	if err := a.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit_log WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	return n
}

// --- H1.5/H2: pre-auth probes -------------------------------------------------

// TestSignOnE2E_PreAuthProbes asserts the cold logged-out posture: the public
// auth-config endpoint advertises OIDC with no credential, while /me is gated.
func TestSignOnE2E_PreAuthProbes(t *testing.T) {
	a := newAuthServer(t, e2eAdminEmail)
	c := a.client(t)

	var cfg struct {
		OIDCEnabled bool `json:"oidc_enabled"`
	}
	if code := a.getJSON(t, c, "/api/v1/auth/config", &cfg); code != http.StatusOK {
		t.Fatalf("/auth/config status = %d, want 200", code)
	}
	if !cfg.OIDCEnabled {
		t.Error("/auth/config oidc_enabled = false, want true")
	}

	if code := a.getJSON(t, c, "/api/v1/me", nil); code != http.StatusUnauthorized {
		t.Errorf("/me without credential = %d, want 401", code)
	}
}

// --- H2 + H3 + admin-grant: the round-trip and its audit trail ---------------

// TestSignOnE2E_OIDCRoundTripAndAudit drives the full sign-on, asserts /me then
// reports the authed principal as admin, and verifies the audit rows: one
// oidc_login + one admin_granted on first login, and on a SECOND login another
// oidc_login but NO second admin_granted (escalation is once-ever).
func TestSignOnE2E_OIDCRoundTripAndAudit(t *testing.T) {
	a := newAuthServer(t, e2eAdminEmail)
	c := a.client(t)

	a.roundTripLogin(t, c)

	var me struct {
		Principal   string `json:"principal"`
		IsAdmin     bool   `json:"is_admin"`
		RBACEnabled bool   `json:"rbac_enabled"`
		OIDCEnabled bool   `json:"oidc_enabled"`
	}
	if code := a.getJSON(t, c, "/api/v1/me", &me); code != http.StatusOK {
		t.Fatalf("/me after login = %d, want 200", code)
	}
	if me.Principal != e2eAdminPrincipal {
		t.Errorf("/me principal = %q, want %q", me.Principal, e2eAdminPrincipal)
	}
	if !me.IsAdmin {
		t.Error("/me is_admin = false, want true (admin_email bootstrap)")
	}
	if !me.RBACEnabled || !me.OIDCEnabled {
		t.Errorf("/me flags rbac=%v oidc=%v, want both true", me.RBACEnabled, me.OIDCEnabled)
	}

	// Audit: exactly one oidc_login and one admin_granted, with the expected
	// kind/source/decision metadata.
	if n := a.auditCount(t,
		"kind=? AND action=? AND principal=? AND source='oidc' AND decision='allow'",
		audit.KindAuthLogin, audit.ActionOIDCLogin, e2eAdminPrincipal); n != 1 {
		t.Errorf("oidc_login rows after first login = %d, want 1", n)
	}
	if n := a.auditCount(t,
		"kind=? AND action=? AND principal=? AND source='admin-bootstrap' AND decision='allow'",
		audit.KindAuthLogin, audit.ActionAdminGranted, e2eAdminPrincipal); n != 1 {
		t.Errorf("admin_granted rows after first login = %d, want 1", n)
	}

	// Second login: a fresh client (new session), same identity.
	a.roundTripLogin(t, a.client(t))

	if n := a.auditCount(t, "action=? AND principal=?",
		audit.ActionOIDCLogin, e2eAdminPrincipal); n != 2 {
		t.Errorf("oidc_login rows after second login = %d, want 2", n)
	}
	if n := a.auditCount(t, "action=? AND principal=?",
		audit.ActionAdminGranted, e2eAdminPrincipal); n != 1 {
		t.Errorf("admin_granted rows after second login = %d, want 1 (escalation is once-ever)", n)
	}
}

// --- H2: logout + server-side revocation -------------------------------------

// TestSignOnE2E_LogoutRevokesServerSide proves logout destroys the session on
// the SERVER, not just in the client: after logout, replaying the original
// session cookie still 401s.
func TestSignOnE2E_LogoutRevokesServerSide(t *testing.T) {
	a := newAuthServer(t, e2eAdminEmail)
	c := a.client(t)

	a.roundTripLogin(t, c)

	// Capture the live session cookie before logout.
	jarURL, _ := url.Parse(a.ts.URL)
	var sessionCookie *http.Cookie
	for _, ck := range c.Jar.Cookies(jarURL) {
		if ck.Name == auth.SessionCookieName {
			sessionCookie = ck
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("no session cookie set after login")
	}

	// Logout (jar carries the session cookie).
	logoutReq, _ := http.NewRequest(http.MethodPost, a.ts.URL+"/api/v1/auth/logout", nil)
	logoutResp, err := c.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST /auth/logout: %v", err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("/auth/logout status = %d, want 200", logoutResp.StatusCode)
	}

	// Replay the OLD cookie on a clean client (no jar state): the server-side
	// session row is gone, so this must 401 rather than silently re-auth.
	stale := a.ts.Client()
	staleReq, _ := http.NewRequest(http.MethodGet, a.ts.URL+"/api/v1/me", nil)
	staleReq.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sessionCookie.Value})
	staleResp, err := stale.Do(staleReq)
	if err != nil {
		t.Fatalf("GET /me with stale cookie: %v", err)
	}
	staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/me with revoked session = %d, want 401 (server-side revocation)", staleResp.StatusCode)
	}
}

// --- H3: break-glass service-account use + windowed dedup --------------------

// TestSignOnE2E_BreakGlassAuditDedup sends several break-glass bearer requests
// over a SINGLE keep-alive connection (one source address) and asserts they
// authenticate AND collapse to exactly one break_glass_use audit row — the
// per-(principal,remote) windowed dedup.
func TestSignOnE2E_BreakGlassAuditDedup(t *testing.T) {
	a := newAuthServer(t, e2eAdminEmail)

	// One connection, reused across sequential requests, so r.RemoteAddr (and
	// thus the dedup key) is identical for all of them.
	c := a.ts.Client()
	if tr, ok := c.Transport.(*http.Transport); ok {
		tr.MaxConnsPerHost = 1
		tr.MaxIdleConnsPerHost = 1
		tr.DisableKeepAlives = false
	}

	const n = 5
	for i := 0; i < n; i++ {
		req, _ := http.NewRequest(http.MethodGet, a.ts.URL+"/api/v1/status", nil)
		req.Header.Set("Authorization", "Bearer "+e2eBreakGlassKey)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("break-glass request %d: %v", i, err)
		}
		// Drain + close so the connection returns to the pool for reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("break-glass request %d status = %d, want 200", i, resp.StatusCode)
		}
	}

	bgPrincipal := "svc:" + e2eBreakGlassName
	if got := a.auditCount(t,
		"kind=? AND action=? AND principal=? AND source='break-glass' AND decision='allow'",
		audit.KindAuthLogin, audit.ActionBreakGlassUse, bgPrincipal); got != 1 {
		t.Errorf("break_glass_use rows after %d requests = %d, want 1 (windowed dedup)", n, got)
	}
}
