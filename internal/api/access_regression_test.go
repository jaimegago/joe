package api_test

import (
	"context"
	"encoding/json"
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
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	_ "modernc.org/sqlite"
)

// mustResolver builds a service-account resolver for the regression chain.
func mustResolver(t *testing.T, accounts ...config.ServiceAccount) *auth.ServiceAccountResolver {
	t.Helper()
	r, err := auth.NewServiceAccountResolver(accounts)
	if err != nil {
		t.Fatalf("NewServiceAccountResolver: %v", err)
	}
	return r
}

// apiFakeK8s is a no-op KubernetesAdapter used so the k8s handlers reach a
// real adapter and return 200 on the happy path.
type apiFakeK8s struct{}

func (apiFakeK8s) Connect(context.Context, store.Source) error { return nil }
func (apiFakeK8s) Disconnect() error                           { return nil }
func (apiFakeK8s) Status() adapters.Status                     { return adapters.Status{Connected: true} }
func (apiFakeK8s) ListResources(context.Context, string, string) ([]unstructured.Unstructured, error) {
	return []unstructured.Unstructured{}, nil
}
func (apiFakeK8s) GetResource(context.Context, string, string, string) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}
func (apiFakeK8s) GetPodLogs(context.Context, string, string, string, int) (string, error) {
	return "", nil
}

func mustRegStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCreateSource(t *testing.T, s *store.Store, id string) {
	t.Helper()
	now := time.Now().UTC()
	if err := s.Sources.Create(context.Background(), &store.Source{
		ID: id, Type: "k8s", Name: id, Config: json.RawMessage(`{}`),
		Status: "connected", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create source %q: %v", id, err)
	}
}

// TestPhaseA_HTTPRBACOutcomesPreserved is the regression contract: with RBAC
// enabled, the HTTP path produces the SAME allow/deny outcomes after Phase A
// (middleware outer gate + accessor authoritative gate) as before it
// (middleware only), for the single configured principal. The granted-read
// case is the load-bearing one: it proves the accessor's declared-action
// classification does not over-deny a request the middleware admitted.
func TestPhaseA_HTTPRBACOutcomesPreserved(t *testing.T) {
	sqlStore := mustRegStore(t)
	ctx := context.Background()

	mustCreateSource(t, sqlStore, "s-allow")
	mustCreateSource(t, sqlStore, "s-deny")

	repo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "s-allow", ZoneID: "prod-readonly", AssignedBy: "test"}); err != nil {
		t.Fatalf("assign s-allow: %v", err)
	}
	if err := repo.UpsertAssignment(ctx, rbac.SourceZoneAssignment{SourceID: "s-deny", ZoneID: "prod-write", AssignedBy: "test"}); err != nil {
		t.Fatalf("assign s-deny: %v", err)
	}
	if _, err := repo.CreatePolicy(ctx, rbac.Policy{Principal: "svc:operator", ZoneID: "prod-readonly"}); err != nil {
		t.Fatalf("grant svc:operator: %v", err)
	}

	registry := adapters.NewRegistry()
	registry.Register("s-allow", apiFakeK8s{})
	registry.Register("s-deny", apiFakeK8s{})

	const apiKey = "secret"
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{ServiceAccounts: []config.ServiceAccount{{Name: "operator", Key: apiKey}}}},
		Store:    sqlStore,
		RBAC:     repo,
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	engine := rbac.NewPolicyEngine(repo)
	// Production-shaped chain: EdgeAuth resolves the service-account key to its
	// svc: principal, EnforcementMiddleware is the outer RBAC gate, and the
	// accessor inside the handlers is the authoritative gate. The Phase A
	// regression contract — middleware + accessor agree for the configured
	// principal — holds identically through the Phase D service-account path.
	handler := api.Chain(mux,
		auth.EdgeAuth(auth.EdgeConfig{ServiceAccounts: mustResolver(t, config.ServiceAccount{Name: "operator", Key: apiKey})}),
		rbac.EnforcementMiddleware(engine),
	)

	cases := []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{"granted read → 200", "/api/v1/k8s/s-allow/resources?resource=pods", "Bearer " + apiKey, http.StatusOK},
		{"ungranted zone → 403", "/api/v1/k8s/s-deny/resources?resource=pods", "Bearer " + apiKey, http.StatusForbidden},
		{"missing token → 401", "/api/v1/k8s/s-allow/resources?resource=pods", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.token != "" {
				r.Header.Set("Authorization", tc.token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Errorf("path %s: got %d, want %d", tc.path, w.Code, tc.want)
			}
		})
	}
}

// TestPhaseA_RBACDisabled_PermitsAll confirms the default (local/dev) posture
// is unchanged: with no API key configured, the policy engine is nil, the
// accessor permits every decision, and requests succeed without a token —
// exactly as before Phase A.
func TestPhaseA_RBACDisabled_PermitsAll(t *testing.T) {
	sqlStore := mustRegStore(t)
	mustCreateSource(t, sqlStore, "s-1")

	registry := adapters.NewRegistry()
	registry.Register("s-1", apiFakeK8s{})

	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{}}, // RBAC disabled (no service accounts)
		Store:    sqlStore,
		RBAC:     rbac.NewRepository(sqlStore.DB(), sqlStore.Driver()),
		Adapters: registry,
	}
	srv := api.New(services)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Mirror main.go: no policy engine when no service account is configured.
	// EdgeAuth in disabled mode resolves every caller to the fallback principal
	// and rejects nothing.
	handler := api.Chain(mux,
		auth.EdgeAuth(auth.EdgeConfig{}),
		rbac.EnforcementMiddleware(nil),
	)

	r := httptest.NewRequest("GET", "/api/v1/k8s/s-1/resources?resource=pods", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("RBAC-disabled k8s read: got %d, want 200", w.Code)
	}
}
