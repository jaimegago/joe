package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// newTestHandlers wires Handlers over an in-memory store with the given fake
// provider and admin email. It returns the handlers, the auth repo, and the
// rbac repo (for asserting bootstrap grants).
func newTestHandlers(t *testing.T, prov Provider, adminEmail string) (*Handlers, *SQLRepository, rbac.Repository, *store.Store) {
	t.Helper()
	repo, s := newTestRepo(t)
	rbacRepo := rbac.NewRepository(s.DB(), s.Driver())
	h := NewHandlers(HandlerConfig{
		Provider:   prov,
		Sessions:   NewSessionManager(repo, time.Hour),
		Repo:       repo,
		RBAC:       rbacRepo,
		Principals: rbacRepo,
		AdminEmail: adminEmail,
	})
	return h, repo, rbacRepo, s
}

// runLogin drives GET /auth/login and returns the state value bound to the
// browser (the state-cookie value).
func runLogin(t *testing.T, h *Handlers) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	w := httptest.NewRecorder()
	h.Login(w, r)
	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d, want 302", resp.StatusCode)
	}
	c := cookieByName(resp, stateCookieName)
	if c == nil || c.Value == "" {
		t.Fatal("login must set a non-empty state cookie")
	}
	return c.Value
}

// TestLogin_StateCookieSecureByDefault asserts the OIDC state cookie is Secure
// out of the box, and that AllowInsecureCookies (the local HTTP dev escape
// hatch) drops Secure so Safari/Firefox can store it over plain http://. Without
// the state cookie the callback's CSRF check fails with a state mismatch — the
// exact symptom this flag fixes.
func TestLogin_StateCookieSecureByDefault(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}

	secure := NewHandlers(HandlerConfig{Provider: prov, Sessions: nil, Repo: mustRepo(t)})
	if c := loginStateCookie(t, secure); !c.Secure {
		t.Error("state cookie must be Secure by default")
	}

	insecure := NewHandlers(HandlerConfig{Provider: prov, Sessions: nil, Repo: mustRepo(t), AllowInsecureCookies: true})
	c := loginStateCookie(t, insecure)
	if c.Secure {
		t.Error("with AllowInsecureCookies the state cookie must NOT be Secure")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly must be unaffected by the insecure-cookie toggle")
	}
}

func mustRepo(t *testing.T) *SQLRepository {
	t.Helper()
	repo, _ := newTestRepo(t)
	return repo
}

func loginStateCookie(t *testing.T, h *Handlers) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	w := httptest.NewRecorder()
	h.Login(w, r)
	c := cookieByName(w.Result(), stateCookieName)
	if c == nil {
		t.Fatal("login must set a state cookie")
	}
	return c
}

// runCallback drives GET /auth/callback with the given state and a matching
// state cookie.
func runCallback(t *testing.T, h *Handlers, state string) *http.Response {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?state="+state+"&code=authcode", nil)
	r.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, r)
	return w.Result()
}

func countSessions(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// TestCallback_SuccessMintsSessionAndPrincipal is the core acceptance: a
// simulated successful OIDC callback yields a session and a user:<email>
// principal.
func TestCallback_SuccessMintsSessionAndPrincipal(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	h, repo, _, s := newTestHandlers(t, prov, "")

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	sc := cookieByName(resp, SessionCookieName)
	if sc == nil || sc.Value == "" {
		t.Fatal("successful callback must set a session cookie")
	}
	if countSessions(t, s) != 1 {
		t.Fatalf("want exactly one session row, got %d", countSessions(t, s))
	}
	got, err := repo.GetSession(context.Background(), sc.Value)
	if err != nil || got == nil {
		t.Fatalf("session row must exist for the cookie: err=%v", err)
	}
	if got.Principal != "user:alice@example.com" {
		t.Fatalf("session principal = %q, want user:alice@example.com", got.Principal)
	}
}

// TestCallback_EmailNotVerifiedRejected: a token with email_verified=false is
// rejected and NO session is created.
func TestCallback_EmailNotVerifiedRejected(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "mallory@example.com", EmailVerified: false}}
	h, _, _, s := newTestHandlers(t, prov, "")

	state := runLogin(t, h)
	resp := runCallback(t, h, state)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unverified email callback status = %d, want 403", resp.StatusCode)
	}
	if sc := cookieByName(resp, SessionCookieName); sc != nil && sc.Value != "" {
		t.Fatal("no session cookie may be set for an unverified email")
	}
	if n := countSessions(t, s); n != 0 {
		t.Fatalf("no session must be created for an unverified email, got %d", n)
	}
}

// TestLogout_RevokesSession: after logout the session is gone (deletion =
// immediate, server-side) and the cookie is cleared.
func TestLogout_RevokesSession(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	h, repo, _, _ := newTestHandlers(t, prov, "")

	state := runLogin(t, h)
	cbResp := runCallback(t, h, state)
	sc := cookieByName(cbResp, SessionCookieName)
	if sc == nil {
		t.Fatal("setup: expected a session cookie from callback")
	}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sc.Value})
	w := httptest.NewRecorder()
	h.Logout(w, r)
	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", resp.StatusCode)
	}
	cleared := cookieByName(resp, SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatal("logout must clear the session cookie (MaxAge < 0)")
	}
	if got, _ := repo.GetSession(context.Background(), sc.Value); got != nil {
		t.Fatal("logout must delete the session row (immediate revocation)")
	}
}

// TestCallback_AdminBootstrap: Phase H (D-0011). The configured admin
// email gains DYNAMIC admin status (an admin_principals row) on first
// login — NOT a snapshot of grants on every zone. A different email
// gains nothing. The Phase C behaviour (snapshot rbac_policies rows
// per zone) is removed; the regression test for it is replaced by the
// Phase H assertions below — the row exists in admin_principals and
// rbac_policies remains empty for the admin.
func TestCallback_AdminBootstrap(t *testing.T) {
	const adminEmail = "admin@example.com"

	t.Run("admin email is marked as dynamic admin", func(t *testing.T) {
		prov := &fakeProvider{claims: Claims{Email: adminEmail, EmailVerified: true}}
		h, _, rbacRepo, _ := newTestHandlers(t, prov, adminEmail)

		state := runLogin(t, h)
		if resp := runCallback(t, h, state); resp.StatusCode != http.StatusFound {
			t.Fatalf("admin callback status = %d, want 302", resp.StatusCode)
		}

		// Dynamic admin row exists.
		isAdmin, err := rbacRepo.IsAdmin(context.Background(), "user:"+adminEmail)
		if err != nil {
			t.Fatalf("IsAdmin: %v", err)
		}
		if !isAdmin {
			t.Fatal("admin email should be marked as dynamic admin after first login")
		}

		// No snapshot rbac_policies rows — single source of truth.
		grants, err := rbacRepo.ListPoliciesForPrincipal(context.Background(), "user:"+adminEmail)
		if err != nil {
			t.Fatalf("list policies: %v", err)
		}
		if len(grants) != 0 {
			t.Fatalf("Phase H: admin must NOT hold rbac_policies snapshot grants, has %d", len(grants))
		}
	})

	t.Run("non-admin email gains nothing", func(t *testing.T) {
		prov := &fakeProvider{claims: Claims{Email: "intern@example.com", EmailVerified: true}}
		h, _, rbacRepo, _ := newTestHandlers(t, prov, adminEmail)

		state := runLogin(t, h)
		if resp := runCallback(t, h, state); resp.StatusCode != http.StatusFound {
			t.Fatalf("non-admin callback status = %d, want 302", resp.StatusCode)
		}

		isAdmin, _ := rbacRepo.IsAdmin(context.Background(), "user:intern@example.com")
		if isAdmin {
			t.Fatal("a non-admin first login must not gain admin status")
		}
		grants, err := rbacRepo.ListPoliciesForPrincipal(context.Background(), "user:intern@example.com")
		if err != nil {
			t.Fatalf("list policies: %v", err)
		}
		if len(grants) != 0 {
			t.Fatalf("a non-admin first login must grant zero zones, got %d", len(grants))
		}
	})
}

// TestCallback_StateMismatchRejected: a callback whose state cookie does not
// match the query state is rejected (login-CSRF guard); no session is created.
func TestCallback_StateMismatchRejected(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	h, _, _, s := newTestHandlers(t, prov, "")

	state := runLogin(t, h)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?state="+state+"&code=authcode", nil)
	r.AddCookie(&http.Cookie{Name: stateCookieName, Value: "a-different-state"})
	w := httptest.NewRecorder()
	h.Callback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("state mismatch status = %d, want 400", w.Result().StatusCode)
	}
	if n := countSessions(t, s); n != 0 {
		t.Fatalf("state mismatch must create no session, got %d", n)
	}
}

// TestCallback_NonceMismatchRejected: an ID token whose nonce does not match the
// flow's issued nonce is rejected. The flow is seeded directly with a known
// nonce; the fake provider (whose AuthCodeURL was never called) returns the
// empty nonce, forcing the mismatch.
func TestCallback_NonceMismatchRejected(t *testing.T) {
	prov := &fakeProvider{claims: Claims{Email: "alice@example.com", EmailVerified: true}}
	h, repo, _, s := newTestHandlers(t, prov, "")

	const state = "seeded-state"
	now := time.Now().UTC()
	if err := repo.CreateFlow(context.Background(), LoginFlow{
		State: state, CodeVerifier: "v", Nonce: "server-issued-nonce",
		CreatedAt: now, ExpiresAt: now.Add(flowTTL),
	}); err != nil {
		t.Fatalf("seed flow: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?state="+state+"&code=authcode", nil)
	r.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	h.Callback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("nonce mismatch status = %d, want 400", w.Result().StatusCode)
	}
	if n := countSessions(t, s); n != 0 {
		t.Fatalf("nonce mismatch must create no session, got %d", n)
	}
}
