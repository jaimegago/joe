package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/rbac"
)

// principalEcho is a downstream handler that writes the context principal, so a
// test can assert which principal EdgeAuth resolved.
func principalEcho(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rbac.PrincipalFromContext(r.Context())))
}

// mustResolver builds a ServiceAccountResolver from the given accounts, failing
// the test on a configuration error.
func mustResolver(t *testing.T, accounts ...config.ServiceAccount) *ServiceAccountResolver {
	t.Helper()
	r, err := NewServiceAccountResolver(accounts)
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}
	return r
}

func TestEdgeAuth_RejectsUnauthenticatedOnProtectedPath(t *testing.T) {
	repo, _ := newTestRepo(t)
	mw := EdgeAuth(EdgeConfig{
		Sessions:       NewSessionManager(repo, time.Hour),
		OIDCConfigured: true, // auth enabled
	})
	h := mw(http.HandlerFunc(principalEcho))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated protected request status = %d, want 401", w.Code)
	}
}

func TestEdgeAuth_SessionCookieResolvesPrincipal(t *testing.T) {
	repo, _ := newTestRepo(t)
	mgr := NewSessionManager(repo, time.Hour)
	mw := EdgeAuth(EdgeConfig{Sessions: mgr, OIDCConfigured: true})
	h := mw(http.HandlerFunc(principalEcho))

	s, err := mgr.Mint(context.Background(), "user:alice@example.com")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: s.ID})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("valid session status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != "user:alice@example.com" {
		t.Fatalf("resolved principal = %q, want user:alice@example.com", got)
	}
}

func TestEdgeAuth_ServiceAccountKeyResolvesPrincipal(t *testing.T) {
	repo, _ := newTestRepo(t)
	resolver := mustResolver(t, config.ServiceAccount{Name: "operator", Key: "secret"})
	mw := EdgeAuth(EdgeConfig{
		Sessions:        NewSessionManager(repo, time.Hour),
		ServiceAccounts: resolver,
	})
	h := mw(http.HandlerFunc(principalEcho))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK || w.Body.String() != "svc:operator" {
		t.Fatalf("service-account resolve = (%d, %q), want (200, svc:operator)", w.Code, w.Body.String())
	}
}

// TestEdgeAuth_UnknownServiceAccountKeyUnauthenticated proves an unknown bearer
// key is treated exactly as an invalid token: 401 on a protected path.
func TestEdgeAuth_UnknownServiceAccountKeyUnauthenticated(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "known"})
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver})
	h := mw(http.HandlerFunc(principalEcho))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
	r.Header.Set("Authorization", "Bearer not-a-configured-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key status = %d, want 401", w.Code)
	}
}

func TestEdgeAuth_LoginPathIsPublic(t *testing.T) {
	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "secret"})
	mw := EdgeAuth(EdgeConfig{ServiceAccounts: resolver}) // auth enabled, no creds on request
	h := mw(http.HandlerFunc(principalEcho))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("login path must bypass auth: status = %d, want 200", w.Code)
	}
}

// TestEdgeAuth_DisabledPermitsAll preserves the pre-Phase-D local/dev posture:
// with neither a service account nor OIDC configured, every caller resolves to
// the fallback principal and nothing is rejected.
func TestEdgeAuth_DisabledPermitsAll(t *testing.T) {
	mw := EdgeAuth(EdgeConfig{}) // disabled
	h := mw(http.HandlerFunc(principalEcho))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK || w.Body.String() != "default-operator" {
		t.Fatalf("disabled mode = (%d, %q), want (200, default-operator)", w.Code, w.Body.String())
	}
}
