//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
)

// These tests exercise the RBAC authorization decision at the guarded accessor
// (internal/access) — the chokepoint Phase E (D-0008) moved enforcement onto,
// reached by both the HTTP handlers and the in-process agent loop. They
// formerly drove a direct-HTTP managed-system route (GET /api/v1/k8s/{id}/
// resources) purely to reach the accessor; that route has been removed, so the
// decision is now asserted directly with constructed principals over a real
// SQLite-backed RBAC repository. The HTTP auth chain (EdgeAuth credential
// extraction → context principal → accessor → status mapping) is covered by the
// api_test suite via a test-only probe route, and by internal/auth.

// newRBACStore opens an in-memory SQLite store with migrations applied and
// returns it alongside an RBAC repository over the same database.
func newRBACStore(t *testing.T) (*store.Store, rbac.Repository) {
	t.Helper()
	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := testStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { testStore.Close() })
	return testStore, rbac.NewRepository(testStore.DB(), testStore.Driver())
}

// registryWithK8s registers a no-op Kubernetes adapter under componentID so an
// allowed read resolves to an adapter and returns nil rather than
// component-not-found.
func registryWithK8s(componentID string) *adapters.Registry {
	registry := adapters.NewRegistry()
	registry.Register(componentID, mocks.NewMockK8sAdapter())
	return registry
}

// TestIntegration_RBAC_NoAuth_SourceReadPassthrough checks that when no service
// account is configured the policy engine is nil and the accessor permits every
// decision (mirroring EnforcementMiddleware(nil) on the old transport) — a read
// is never denied by RBAC.
func TestIntegration_RBAC_NoAuth_SourceReadPassthrough(t *testing.T) {
	// nil engine ⇒ accessor permits all; the registered adapter makes the read
	// reach infrastructure and return nil.
	acc := access.New(registryWithK8s("local-k8s"), nil, nil, nil)

	if _, err := acc.K8sListResources(context.Background(), "anyone", "local-k8s", "pods", ""); err != nil {
		if errors.Is(err, access.ErrPermissionDenied) {
			t.Errorf("auth disabled (nil engine): read must not be denied by RBAC, got ErrPermissionDenied")
		}
		t.Errorf("auth disabled: read should reach the adapter, got: %v", err)
	}
}

// TestIntegration_RBAC_Auth_AllowsReadWithPolicy checks that a principal whose
// policy grants the source's zone is allowed by the accessor.
func TestIntegration_RBAC_Auth_AllowsReadWithPolicy(t *testing.T) {
	testStore, repo := newRBACStore(t)
	ctx := context.Background()
	const principal rbac.Principal = "svc:ops"

	if err := testStore.Components.Create(ctx, &store.Component{
		ID: "local-k8s", Type: store.ComponentTypeKubernetes, Name: "Local Kind", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	// Seed: local-k8s → prod-readonly; svc:ops → prod-readonly.
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{
		Principal: string(principal), ZoneID: "prod-readonly",
	}, "test"); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	acc := access.New(registryWithK8s("local-k8s"), nil, rbac.NewPolicyEngine(repo), nil)
	if _, err := acc.K8sListResources(ctx, principal, "local-k8s", "pods", ""); err != nil {
		t.Errorf("principal with a valid policy should be allowed, got: %v", err)
	}
}

// TestIntegration_RBAC_Auth_DeniesReadWithoutPolicy checks that a principal with
// no policy for the source's zone is denied with ErrPermissionDenied. This is
// the security-load-bearing deny assertion (previously asserted as an HTTP 403).
func TestIntegration_RBAC_Auth_DeniesReadWithoutPolicy(t *testing.T) {
	testStore, repo := newRBACStore(t)
	ctx := context.Background()

	if err := testStore.Components.Create(ctx, &store.Component{
		ID: "local-k8s", Type: store.ComponentTypeKubernetes, Name: "Local Kind", Config: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}
	// Assign the source to a zone but grant svc:nobody no policy for it.
	if err := repo.UpsertAssignment(ctx, rbac.ComponentZoneAssignment{
		ComponentID: "local-k8s", ZoneID: "prod-readonly", AssignedBy: "test",
	}, "test"); err != nil {
		t.Fatalf("UpsertAssignment: %v", err)
	}

	acc := access.New(registryWithK8s("local-k8s"), nil, rbac.NewPolicyEngine(repo), nil)
	if _, err := acc.K8sListResources(ctx, "svc:nobody", "local-k8s", "pods", ""); !errors.Is(err, access.ErrPermissionDenied) {
		t.Errorf("principal with no policy must be denied with ErrPermissionDenied, got: %v", err)
	}
}
