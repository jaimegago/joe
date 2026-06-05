//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// rbacTestEnv holds the components needed to build varied middleware stacks.
// After Phase D (D-0007) BearerAuth + APIKeyProvider were deleted; after Phase
// E (D-0008) EnforcementMiddleware was demoted to a pass-through. The
// production-shaped chain here mirrors cmd/joe/server.go: auth.EdgeAuth
// resolves the caller principal from a service-account bearer key, and the
// guarded accessor (inside the handlers) is the sole authoritative RBAC gate.
type rbacTestEnv struct {
	store *store.Store
	repo  rbac.Repository
	mux   *http.ServeMux
}

func newRBACEnv(t *testing.T, accounts ...config.ServiceAccount) *rbacTestEnv {
	t.Helper()

	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := testStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { testStore.Close() })

	repo := rbac.NewRepository(testStore.DB(), testStore.Driver())

	cfg := &config.Config{Server: config.ServerConfig{ServiceAccounts: accounts}}
	services := core.New(cfg, testStore, testStore.DB(), testStore.Driver(), adapters.NewRegistry(), nil)
	services.RBAC = repo

	mux := http.NewServeMux()
	api.New(services).RegisterRoutes(mux)

	return &rbacTestEnv{store: testStore, repo: repo, mux: mux}
}

// buildHandler wraps env.mux with auth.EdgeAuth (the production edge gate),
// matching cmd/joe/server.go. The accessor inside the handlers is now the
// sole RBAC gate (Phase E demotion).
func (e *rbacTestEnv) buildHandler(accounts ...config.ServiceAccount) http.Handler {
	resolver, err := auth.NewServiceAccountResolver(accounts)
	if err != nil {
		panic(err)
	}
	return api.Chain(
		e.mux,
		auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: resolver}),
	)
}

// TestIntegration_RBAC_NoAuth_SourceReadPassthrough checks that when no
// service account is configured the policy engine is nil and GET requests to
// source-scoped paths are never blocked by RBAC (they may still return
// 404/501 for missing adapters).
func TestIntegration_RBAC_NoAuth_SourceReadPassthrough(t *testing.T) {
	env := newRBACEnv(t) // no service accounts ⇒ engine nil ⇒ accessor permits

	ctx := context.Background()
	if err := env.store.Sources.Create(ctx, &store.Source{
		ID: "local-k8s", Type: store.SourceTypeKubernetes, Name: "Local Kind",
		Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	handler := env.buildHandler() // no accounts ⇒ auth disabled

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/local-k8s/resources?resource=pods", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Errorf("expected non-403 when auth is disabled, got 403 — RBAC gate should be open")
	}
}

// TestIntegration_RBAC_Auth_AllowsReadWithPolicy checks that a principal who
// has a policy granting access to the source's zone can perform a GET.
func TestIntegration_RBAC_Auth_AllowsReadWithPolicy(t *testing.T) {
	const apiKey = "test-secret"
	account := config.ServiceAccount{Name: "ops", Key: apiKey}
	const principal rbac.Principal = "svc:ops"

	env := newRBACEnv(t, account)
	ctx := context.Background()

	if err := env.store.Sources.Create(ctx, &store.Source{
		ID: "local-k8s", Type: store.SourceTypeKubernetes, Name: "Local Kind",
		Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Seed: local-k8s → prod-readonly; svc:ops → prod-readonly.
	if err := env.repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}
	if _, err := env.repo.CreatePolicy(ctx, rbac.Policy{
		Principal: string(principal), ZoneID: "prod-readonly",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	handler := env.buildHandler(account)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/local-k8s/resources?resource=pods", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Errorf("expected non-403 for principal with valid policy, got 403")
	}
}

// TestIntegration_RBAC_Auth_DeniesReadWithoutPolicy checks that a principal
// with no policy for the source's zone receives 403 when auth is enabled.
func TestIntegration_RBAC_Auth_DeniesReadWithoutPolicy(t *testing.T) {
	const apiKey = "test-secret"
	account := config.ServiceAccount{Name: "nobody", Key: apiKey}

	env := newRBACEnv(t, account)
	ctx := context.Background()

	if err := env.store.Sources.Create(ctx, &store.Source{
		ID: "local-k8s", Type: store.SourceTypeKubernetes, Name: "Local Kind",
		Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Assign source to a zone but grant no policy to svc:nobody.
	if err := env.repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	handler := env.buildHandler(account)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/local-k8s/resources?resource=pods", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for principal with no policy, got %d", w.Code)
	}
}
