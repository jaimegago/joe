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

// TestPhaseD_TwoServiceAccountsIndependentZones is the headline Phase D
// acceptance: two distinct configured service-account keys resolve to two
// distinct svc: principals, and each is allowed only on its own granted zone
// and denied on the other's — proven through the guarded accessor.
//
// EnforcementMiddleware is omitted so the accessor is the sole RBAC gate
// (mirroring the Phase B/C isolation tests): the 200/403 outcome therefore
// proves the svc: principal EdgeAuth resolved from the key reached the accessor
// decision specifically.
func TestPhaseD_TwoServiceAccountsIndependentZones(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateSource(t, sqlStore, "s-ci")  // granted to svc:ci only
	mustCreateSource(t, sqlStore, "s-mcp") // granted to svc:mcp only

	rbacRepo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := rbacRepo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "s-ci", ZoneID: "prod-readonly", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("assign s-ci: %v", err)
	}
	if err := rbacRepo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "s-mcp", ZoneID: "dev-full", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("assign s-mcp: %v", err)
	}
	// Independent grants: svc:ci → prod-readonly, svc:mcp → dev-full.
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: "svc:ci", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant svc:ci: %v", err)
	}
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: "svc:mcp", ZoneID: "dev-full"}, "test"); err != nil {
		t.Fatalf("grant svc:mcp: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-ci", apiFakeK8s{})
	registry.Register("s-mcp", apiFakeK8s{})

	accounts := []config.ServiceAccount{{Name: "ci", Key: "key-ci"}, {Name: "mcp", Key: "key-mcp"}}
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: accounts}},
		Store:    sqlStore,
		RBAC:     rbacRepo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// EdgeAuth is the only auth layer; the accessor inside the handler is the
	// only RBAC gate (no EnforcementMiddleware).
	handler := auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: mustResolver(t, accounts...)})(mux)

	call := func(bearerKey, sourceID string) int {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/"+sourceID+"/resources?resource=pods", nil)
		r.Header.Set("Authorization", "Bearer "+bearerKey)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}

	// svc:ci allowed on its zone, denied on svc:mcp's.
	if code := call("key-ci", "s-ci"); code != http.StatusOK {
		t.Errorf("svc:ci on s-ci (prod-readonly, granted): got %d, want 200", code)
	}
	if code := call("key-ci", "s-mcp"); code != http.StatusForbidden {
		t.Errorf("svc:ci on s-mcp (dev-full, ungranted): got %d, want 403", code)
	}
	// svc:mcp allowed on its zone, denied on svc:ci's — independent grants.
	if code := call("key-mcp", "s-mcp"); code != http.StatusOK {
		t.Errorf("svc:mcp on s-mcp (dev-full, granted): got %d, want 200", code)
	}
	if code := call("key-mcp", "s-ci"); code != http.StatusForbidden {
		t.Errorf("svc:mcp on s-ci (prod-readonly, ungranted): got %d, want 403", code)
	}
}

// TestPhaseD_UnknownKeyUnauthenticated proves an unknown service-account key is
// rejected on a protected path through the production-shaped chain, exactly as
// an invalid bearer token would be.
func TestPhaseD_UnknownKeyUnauthenticated(t *testing.T) {
	sqlStore := mustRegStore(t)
	registry := adapters.NewRegistry()
	registry.Register("s1", apiFakeK8s{})

	accounts := []config.ServiceAccount{{Name: "ci", Key: "known"}}
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: accounts}},
		Store:    sqlStore,
		RBAC:     rbac.NewRepository(sqlStore.DB(), sqlStore.Driver()),
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: mustResolver(t, accounts...)})(mux)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources?resource=pods", nil)
	r.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown service-account key: got %d, want 401", w.Code)
	}
}

// TestPhaseD_ZeroZoneDeniedThenGrantAllows proves a service-account principal
// with no granted zones is denied all infrastructure, and that a CLI-equivalent
// zone grant (an rbac_policies row keyed on the svc: principal — what
// `joe zone grant --principal svc:ci` writes) then admits it on the granted
// zone, through the accessor.
func TestPhaseD_ZeroZoneDeniedThenGrantAllows(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateSource(t, sqlStore, "s-allow")
	rbacRepo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := rbacRepo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "s-allow", ZoneID: "prod-readonly", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("assign s-allow: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-allow", apiFakeK8s{})

	accounts := []config.ServiceAccount{{Name: "ci", Key: "key-ci"}}
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: accounts}},
		Store:    sqlStore,
		RBAC:     rbacRepo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: mustResolver(t, accounts...)})(mux)

	call := func() int {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s-allow/resources?resource=pods", nil)
		r.Header.Set("Authorization", "Bearer key-ci")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}

	// Zero zones → denied.
	if code := call(); code != http.StatusForbidden {
		t.Errorf("zero-zone svc:ci: got %d, want 403", code)
	}
	// CLI-equivalent grant: svc:ci → prod-readonly.
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: "svc:ci", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant svc:ci: %v", err)
	}
	if code := call(); code != http.StatusOK {
		t.Errorf("after grant, svc:ci on s-allow: got %d, want 200", code)
	}
}

// TestPhaseD_OIDCAndServiceKeyCoexist proves the two authentication mechanisms
// coexist on the same protected endpoint and converge on a single principal in
// context: a human session cookie yields the user: principal, a service-account
// bearer key yields the svc: principal, and when a request carries BOTH the
// session cookie wins (deterministic, documented precedence — the human
// session takes precedence over the machine key).
func TestPhaseD_OIDCAndServiceKeyCoexist(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	authRepo := auth.NewRepository(sqlStore.DB(), sqlStore.Driver())
	sessions := auth.NewSessionManager(authRepo, time.Hour)
	const human = rbac.Principal("user:alice@example.com")
	session, err := sessions.Mint(ctx, human)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}

	resolver := mustResolver(t, config.ServiceAccount{Name: "ci", Key: "key-ci"})
	// Both mechanisms configured; OIDCConfigured marks the edge enabled even
	// for cookie-only callers.
	handler := auth.EdgeAuth(auth.EdgeConfig{
		Sessions:        sessions,
		ServiceAccounts: resolver,
		OIDCConfigured:  true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rbac.PrincipalFromContext(r.Context())))
	}))

	resolve := func(withCookie, withBearer bool) (int, string) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s1/resources", nil)
		if withCookie {
			r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: session.ID})
		}
		if withBearer {
			r.Header.Set("Authorization", "Bearer key-ci")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	// Human session alone → user: principal.
	if code, p := resolve(true, false); code != http.StatusOK || p != string(human) {
		t.Errorf("session-only = (%d, %q), want (200, %q)", code, p, human)
	}
	// Service-account key alone → svc: principal.
	if code, p := resolve(false, true); code != http.StatusOK || p != "svc:ci" {
		t.Errorf("bearer-only = (%d, %q), want (200, svc:ci)", code, p)
	}
	// Both present → session (human) wins, deterministically.
	if code, p := resolve(true, true); code != http.StatusOK || p != string(human) {
		t.Errorf("cookie+bearer = (%d, %q), want (200, %q) — session must take precedence", code, p, human)
	}
}

// TestPhaseD_ColocatedServerKeyReachesInfra proves the co-located server
// service-account key still authenticates and resolves to svc:server through
// the production-shaped auth chain. After Phase E removed the in-process
// loopback, the joe CLI and REPL — separate external processes that ship with
// the binary — remain co-located HTTP clients that present this key (via
// ServerConfig.LoopbackKey, retained for that consumer). This test still
// exercises the auth/RBAC chain end-to-end: denied (403) before the zone
// grant, allowed (200) after. The earlier name referenced "loopback" because
// the in-process loop used to traverse this same path; with Phase E that
// usage is gone but the external CLI client path remains.
func TestPhaseD_ColocatedServerKeyReachesInfra(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateSource(t, sqlStore, "s-infra")
	rbacRepo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := rbacRepo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "s-infra", ZoneID: "prod-readonly", AssignedBy: "test"}, "test"); err != nil {
		t.Fatalf("assign s-infra: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-infra", apiFakeK8s{})

	// The reserved "server" service account is what LoopbackKey returns.
	srvCfg := config.ServerConfig{ServiceAccounts: []config.ServiceAccount{{Name: "server", Key: "colocated-key"}}}
	if got := srvCfg.LoopbackKey(); got != "colocated-key" {
		t.Fatalf("LoopbackKey() = %q, want colocated-key", got)
	}

	services := &core.Services{
		Config:   &config.Config{Server: srvCfg},
		Store:    sqlStore,
		RBAC:     rbacRepo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	engine := rbac.NewPolicyEngine(rbacRepo)
	handler := api.Chain(mux,
		auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: mustResolver(t, srvCfg.ServiceAccounts...)}),
		rbac.EnforcementMiddleware(engine),
	)

	call := func() int {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/s-infra/resources?resource=pods", nil)
		r.Header.Set("Authorization", "Bearer "+srvCfg.LoopbackKey())
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}

	// Before any grant, svc:server cannot reach infra.
	if code := call(); code != http.StatusForbidden {
		t.Errorf("ungranted co-located key (svc:server): got %d, want 403", code)
	}
	// Grant svc:server the zone — the CLI's path through joe reaches infra
	// end-to-end via the accessor (the only RBAC gate after Phase E).
	if _, err := rbacRepo.CreatePolicy(ctx, rbac.Policy{Principal: "svc:server", ZoneID: "prod-readonly"}, "test"); err != nil {
		t.Fatalf("grant svc:server: %v", err)
	}
	if code := call(); code != http.StatusOK {
		t.Errorf("granted co-located key (svc:server) reaching infra: got %d, want 200", code)
	}
}
