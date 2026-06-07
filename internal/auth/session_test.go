package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/rbac"
)

func TestSessionManager_MintResolveRevoke(t *testing.T) {
	repo, _ := newTestRepo(t)
	mgr := NewSessionManager(repo, time.Hour)
	ctx := context.Background()

	s, err := mgr.Mint(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if s.ID == "" {
		t.Fatal("minted session must have a non-empty id")
	}

	p, ok := mgr.Resolve(ctx, s.ID)
	if !ok || p != rbac.Principal("user:alice@example.com") {
		t.Fatalf("resolve = (%q, %v), want (user:alice@example.com, true)", p, ok)
	}

	// Logout / revocation deletes the row → immediately unauthenticated.
	if err := mgr.Revoke(ctx, s.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := mgr.Resolve(ctx, s.ID); ok {
		t.Fatal("a revoked session must not resolve (logout = immediate)")
	}
}

// TestSessionManager_ExpiredRejected proves a session past its bounded lifetime
// is rejected even though the row still exists at lookup time — the manager
// enforces the expiry against its clock (design §2.3: not accepted past expiry).
func TestSessionManager_ExpiredRejected(t *testing.T) {
	repo, _ := newTestRepo(t)
	mgr := NewSessionManager(repo, time.Hour)
	ctx := context.Background()

	// Pin a clock in the past for minting, then advance it past the TTL for the
	// resolve so expiry is deterministic.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mgr.now = func() time.Time { return base }
	s, err := mgr.Mint(ctx, "user:carol@example.com")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	mgr.now = func() time.Time { return base.Add(2 * time.Hour) } // past the 1h TTL
	if _, ok := mgr.Resolve(ctx, s.ID); ok {
		t.Fatal("a session past its bounded lifetime must be rejected")
	}
}

func TestSessionManager_TTLNeverUnbounded(t *testing.T) {
	repo, _ := newTestRepo(t)
	// A non-positive TTL must fall back to a bounded default, never "forever".
	mgr := NewSessionManager(repo, 0)
	if mgr.ttl <= 0 {
		t.Fatalf("session TTL must be bounded; got %v", mgr.ttl)
	}
}

// TestSessionManager_CookieAttributes is the SameSite=Lax acceptance: the
// session cookie is exactly HttpOnly + Secure + SameSite=Lax. Lax (not Strict)
// is required so the cookie survives the cross-site top-level GET navigation
// returning from the IdP to the callback; a true cross-site redirect cannot be
// simulated in an httptest harness, so we assert the attributes that make that
// redirect work and document why (design §2.3).
func TestSessionManager_CookieAttributes(t *testing.T) {
	repo, _ := newTestRepo(t)
	mgr := NewSessionManager(repo, time.Hour)

	s, err := mgr.Mint(context.Background(), "user:dave@example.com")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	rec := httptest.NewRecorder()
	mgr.SetCookie(rec, s)
	c := cookieByName(rec.Result(), SessionCookieName)
	if c == nil {
		t.Fatal("session cookie not set")
	}
	if !c.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if !c.Secure {
		t.Error("session cookie must be Secure")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax (Strict would break the OIDC callback)", c.SameSite)
	}
	if c.Value != s.ID {
		t.Errorf("cookie carries %q, want the opaque session id %q", c.Value, s.ID)
	}
}

// TestSessionManager_InsecureCookies proves SetSecureCookies(false) drops the
// Secure attribute on both the set and the clear cookie — the local HTTP dev
// escape hatch (ServerConfig.InsecureCookies) so Safari/Firefox, which refuse
// Secure cookies over plain http://, can store the session. HttpOnly and
// SameSite are unaffected.
func TestSessionManager_InsecureCookies(t *testing.T) {
	repo, _ := newTestRepo(t)
	mgr := NewSessionManager(repo, time.Hour)
	mgr.SetSecureCookies(false)

	s, err := mgr.Mint(context.Background(), "user:dave@example.com")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	setRec := httptest.NewRecorder()
	mgr.SetCookie(setRec, s)
	set := cookieByName(setRec.Result(), SessionCookieName)
	if set == nil {
		t.Fatal("session cookie not set")
	}
	if set.Secure {
		t.Error("with insecure cookies enabled the session cookie must NOT be Secure")
	}
	if !set.HttpOnly {
		t.Error("HttpOnly must be unaffected by the insecure-cookie toggle")
	}
	if set.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite must stay Lax, got %v", set.SameSite)
	}

	clearRec := httptest.NewRecorder()
	mgr.ClearCookie(clearRec)
	cleared := cookieByName(clearRec.Result(), SessionCookieName)
	if cleared == nil {
		t.Fatal("clear cookie not set")
	}
	if cleared.Secure {
		t.Error("the clear cookie must match the set cookie's Secure=false, or the browser keeps the stale cookie")
	}
}
