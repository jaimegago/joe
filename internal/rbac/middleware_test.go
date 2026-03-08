package rbac_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// staticProvider always returns the given principal.
type staticProvider struct{ p rbac.Principal }

func (s *staticProvider) Identity(_ *http.Request) rbac.Principal { return s.p }

func TestIdentityMiddleware_InjectsContext(t *testing.T) {
	var captured rbac.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Access via context — package-private, so we can only test indirectly.
		// We check that the middleware does not break the handler chain.
		captured = "read-via-handler"
		w.WriteHeader(http.StatusOK)
	})

	mw := rbac.IdentityMiddleware(&staticProvider{"alice"})
	handler := mw(next)

	r := httptest.NewRequest("GET", "/api/v1/k8s/cluster-1/resources", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	_ = captured // accessed inside handler
}

func TestEnforcementMiddleware_AllowsWhenNoSourceID(t *testing.T) {
	// Paths without a sourceID (e.g. /api/v1/status) should pass through.
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	engine := rbac.NewPolicyEngine(repo)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := rbac.EnforcementMiddleware(engine)
	handler := mw(next)

	r := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for no-sourceID path, got %d", w.Code)
	}
}

func TestEnforcementMiddleware_AllowsAccessWithPolicy(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// k8s-prod in prod-readonly; alice has access.
	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"})

	engine := rbac.NewPolicyEngine(repo)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with identity (alice) + enforcement.
	handler := rbac.IdentityMiddleware(&staticProvider{"alice"})(
		rbac.EnforcementMiddleware(engine)(next),
	)

	r := httptest.NewRequest("GET", "/api/v1/k8s/k8s-prod/resources", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed access, got %d", w.Code)
	}
}

func TestEnforcementMiddleware_DeniesWithoutPolicy(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	// k8s-prod in prod-readonly; eve has no policy.
	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})

	engine := rbac.NewPolicyEngine(repo)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rbac.IdentityMiddleware(&staticProvider{"eve"})(
		rbac.EnforcementMiddleware(engine)(next),
	)

	r := httptest.NewRequest("GET", "/api/v1/k8s/k8s-prod/resources", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for no-policy access, got %d", w.Code)
	}
}

func TestEnforcementMiddleware_DeniesWriteOnReadOnlyZone(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	ctx := context.Background()

	_ = repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{
		SourceID: "k8s-prod", ZoneID: "prod-readonly", AssignedBy: "test",
	})
	_, _ = repo.CreatePolicy(ctx, rbac.Policy{Principal: "alice", ZoneID: "prod-readonly"})

	engine := rbac.NewPolicyEngine(repo)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rbac.IdentityMiddleware(&staticProvider{"alice"})(
		rbac.EnforcementMiddleware(engine)(next),
	)

	// POST = mutate — not allowed in prod-readonly.
	r := httptest.NewRequest("POST", "/api/v1/k8s/k8s-prod/resources", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for write on read-only zone, got %d", w.Code)
	}
}

func TestEnforcementMiddleware_NilEngine_Passthrough(t *testing.T) {
	// nil engine means enforcement is disabled.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rbac.EnforcementMiddleware(nil)(next)

	r := httptest.NewRequest("DELETE", "/api/v1/k8s/k8s-prod/resources/pod/default/my-pod", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with nil engine (disabled), got %d", w.Code)
	}
}
