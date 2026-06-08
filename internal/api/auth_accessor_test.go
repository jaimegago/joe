package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
)

// TestPhaseC_OIDCSessionPrincipalReachesAccessor is the Phase C end-to-end
// acceptance: a session-cookie-borne user:<email> principal (the shape the OIDC
// callback mints) flows through EdgeAuth into the request context and reaches
// the guarded accessor's decision. A freshly-provisioned user with zero zones
// is denied all infrastructure; after a CLI-equivalent zone grant the same user
// is allowed on the granted zone and still denied elsewhere.
//
// EnforcementMiddleware is deliberately omitted so the accessor is the sole RBAC
// gate (mirroring the Phase B isolation test): the 200/403 outcome therefore
// proves the context principal reached the accessor specifically.
func TestPhaseC_OIDCSessionPrincipalReachesAccessor(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateComponent(t, sqlStore, "s-allow") // will be granted
	mustCreateComponent(t, sqlStore, "s-other") // never granted

	rbacRepo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := rbacRepo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "s-allow", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign s-allow: %v", err)
	}
	if err := rbacRepo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "s-other", ZoneID: "prod-write", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("assign s-other: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-allow", apiFakeK8s{})
	registry.Register("s-other", apiFakeK8s{})

	// Service account set ⇒ the accessor's policy engine is non-nil and enforces.
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: []config.ServiceAccount{{Name: "operator", Key: "secret"}}}},
		Store:    sqlStore,
		RBAC:     rbacRepo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// A session for the OIDC-derived principal, backed by the same store.
	authRepo := auth.NewRepository(sqlStore.DB(), sqlStore.Driver())
	sessions := auth.NewSessionManager(authRepo, time.Hour)
	const principal = rbac.Principal("user:alice@example.com")
	session, err := sessions.Mint(ctx, principal)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}

	// EdgeAuth is the only auth layer; the accessor inside the handler is the
	// only RBAC gate. OIDCConfigured=true marks auth enabled.
	handler := auth.EdgeAuth(auth.EdgeConfig{Sessions: sessions, OIDCConfigured: true})(mux)

	call := func(sourceID string) int {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/"+sourceID+"/resources?resource=pods", nil)
		r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: session.ID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}

	// Zero zones → denied everywhere.
	if code := call("s-allow"); code != http.StatusForbidden {
		t.Errorf("zero-zone user on s-allow: got %d, want 403", code)
	}
	if code := call("s-other"); code != http.StatusForbidden {
		t.Errorf("zero-zone user on s-other: got %d, want 403", code)
	}

	// CLI-equivalent grant: user:alice@example.com → prod-readonly.
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: string(principal), ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant prod-readonly: %v", err)
	}

	// Granted zone → allowed; ungranted zone → still denied.
	if code := call("s-allow"); code != http.StatusOK {
		t.Errorf("after grant, user on s-allow (prod-readonly): got %d, want 200", code)
	}
	if code := call("s-other"); code != http.StatusForbidden {
		t.Errorf("after grant, user on s-other (prod-write, ungranted): got %d, want 403", code)
	}
}

// TestPhaseC_ExpiredSessionTreatedAsUnauthenticated proves an expired session
// is rejected on a protected path exactly as an unauthenticated request — the
// edge returns 401, not a stale-but-accepted 200/403.
func TestPhaseC_ExpiredSessionTreatedAsUnauthenticated(t *testing.T) {
	sqlStore := mustRegStore(t)
	authRepo := auth.NewRepository(sqlStore.DB(), sqlStore.Driver())
	sessions := auth.NewSessionManager(authRepo, time.Hour)

	// Mint a session, then directly age it past expiry by writing an expired row.
	if err := authRepo.CreateSession(context.Background(), auth.Session{
		ID:        "expired-id",
		Principal: "user:alice@example.com",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed expired session: %v", err)
	}

	handler := auth.EdgeAuth(auth.EdgeConfig{Sessions: sessions, OIDCConfigured: true})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "expired-id"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session must be treated as unauthenticated: got %d, want 401", w.Code)
	}
}
