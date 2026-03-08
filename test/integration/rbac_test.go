//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// rbacTestEnv holds the components needed to build varied middleware stacks.
type rbacTestEnv struct {
	store *store.Store
	repo  rbac.Repository
	mux   *http.ServeMux
}

func newRBACEnv(t *testing.T) *rbacTestEnv {
	t.Helper()

	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := testStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { testStore.Close() })

	repo := rbac.NewRepository(testStore.DB())

	services := core.New(&config.Config{}, testStore, testStore.DB(), testStore.Driver(), adapters.NewRegistry(), nil)
	services.RBAC = repo

	mux := http.NewServeMux()
	api.New(services).RegisterRoutes(mux)

	return &rbacTestEnv{store: testStore, repo: repo, mux: mux}
}

// buildHandler wraps env.mux with BearerAuth → IdentityMiddleware →
// EnforcementMiddleware, mirroring cmd/joecored/main.go.
// A nil policyEngine disables enforcement (auth-off case).
func (e *rbacTestEnv) buildHandler(apiKey string, principal rbac.Principal, engine *rbac.PolicyEngine) http.Handler {
	return api.Chain(
		e.mux,
		api.BearerAuth(apiKey),
		rbac.IdentityMiddleware(rbac.NewAPIKeyProvider(apiKey, principal)),
		rbac.EnforcementMiddleware(engine),
	)
}

// TestIntegration_RBAC_NoAuth_SourceReadPassthrough checks that when api_key is
// empty the policy engine is nil and GET requests to source-scoped paths are
// never blocked by RBAC (they may still return 404/501 for missing adapters).
func TestIntegration_RBAC_NoAuth_SourceReadPassthrough(t *testing.T) {
	env := newRBACEnv(t)

	ctx := context.Background()
	if err := env.store.Sources.Create(ctx, &store.Source{
		ID: "local-k8s", Type: store.SourceTypeKubernetes, Name: "Local Kind",
		Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	// nil engine mirrors the fix: policyEngine is only created when api_key != "".
	handler := env.buildHandler("", "default-operator", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/local-k8s/resources", nil)
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
	const principal rbac.Principal = "ops-team"

	env := newRBACEnv(t)
	ctx := context.Background()

	// Source must exist before the FK-constrained zone assignment.
	if err := env.store.Sources.Create(ctx, &store.Source{
		ID: "local-k8s", Type: store.SourceTypeKubernetes, Name: "Local Kind",
		Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Seed: local-k8s → prod-readonly; ops-team → prod-readonly.
	if err := env.repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}
	if _, err := env.repo.CreatePolicy(ctx, rbac.Policy{
		Principal: string(principal), ZoneID: "prod-readonly",
	}); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	handler := env.buildHandler(apiKey, principal, rbac.NewPolicyEngine(env.repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/local-k8s/resources", nil)
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
	const principal rbac.Principal = "no-policy-user"

	env := newRBACEnv(t)
	ctx := context.Background()

	// Source must exist before the FK-constrained zone assignment.
	if err := env.store.Sources.Create(ctx, &store.Source{
		ID: "local-k8s", Type: store.SourceTypeKubernetes, Name: "Local Kind",
		Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	// Assign source to a zone but grant no policy to the principal.
	if err := env.repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	handler := env.buildHandler(apiKey, principal, rbac.NewPolicyEngine(env.repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/k8s/local-k8s/resources", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for principal with no policy, got %d", w.Code)
	}
}
