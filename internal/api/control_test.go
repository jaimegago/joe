package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// mockCoreAgent is a test double for the CoreAgent interface
type mockCoreAgent struct {
	onboardingErr       error
	refreshErr          error
	refreshComponentErr error
	onboardingCalled    bool
	refreshCalled       bool
	refreshComponentID  string
	onboardingInput     string
}

func (m *mockCoreAgent) ProcessOnboarding(ctx context.Context, input string) error {
	m.onboardingCalled = true
	m.onboardingInput = input
	return m.onboardingErr
}

func (m *mockCoreAgent) TriggerRefresh(ctx context.Context) error {
	m.refreshCalled = true
	return m.refreshErr
}

func (m *mockCoreAgent) TriggerRefreshComponent(ctx context.Context, sourceID string) error {
	m.refreshComponentID = sourceID
	return m.refreshComponentErr
}

func setupControlTestServer(t *testing.T, agent core.CoreAgent) *Server {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	services := &core.Services{
		Config:   &config.Config{},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
		Agent:    agent,
	}

	return New(services)
}

// TestOnboardingRouteParked asserts the parked contract (D-0081): the onboarding
// HTTP route is unregistered for launch, so a request to it 404s through the mux
// — even when a CoreAgent is wired. The handleOnboarding handler is retained but
// no longer reachable via RegisterRoutes. Handler-level behavior is unchanged;
// this test only pins that the route no longer resolves.
func TestOnboardingRouteParked(t *testing.T) {
	server := setupControlTestServer(t, &mockCoreAgent{})
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/onboarding", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (route parked, D-0081)", rec.Code, http.StatusNotFound)
	}
}

// TestRefreshRouteParked asserts the parked contract (D-0081): the manual-refresh
// HTTP route is unregistered for launch, so a request to it 404s through the mux
// — even when a CoreAgent is wired. The autonomous Refresher is launched from
// Agent.Start and does not depend on this route, so parking it does not affect
// the background refresh loop.
func TestRefreshRouteParked(t *testing.T) {
	server := setupControlTestServer(t, &mockCoreAgent{})
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/refresh", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (route parked, D-0081)", rec.Code, http.StatusNotFound)
	}
}
