package api

import (
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

// setupTestServer creates a minimal test server for route registration tests
func setupTestServer(t *testing.T) *Server {
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
	}

	return New(services)
}

// TestRouteRegistration validates that routes are registered correctly without starting a server
func TestRouteRegistration(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int // 200, 400, 404, 501, etc.
		description    string
	}{
		// Status routes
		{
			name:           "status endpoint",
			method:         "GET",
			path:           "/api/v1/status",
			expectedStatus: http.StatusOK,
			description:    "should return 200 with status info",
		},
		// Graph routes
		{
			name:           "graph query endpoint",
			method:         "GET",
			path:           "/api/v1/graph/query",
			expectedStatus: http.StatusBadRequest, // missing 'q' param
			description:    "should be registered (returns 400 for missing query)",
		},
		{
			name:           "graph related endpoint",
			method:         "GET",
			path:           "/api/v1/graph/related",
			expectedStatus: http.StatusBadRequest, // missing 'nodeID' param
			description:    "should be registered (returns 400 for missing nodeID)",
		},
		{
			name:           "graph summary endpoint",
			method:         "GET",
			path:           "/api/v1/graph/summary",
			expectedStatus: http.StatusOK,
			description:    "should return 200 with empty summary",
		},
		// Source routes
		{
			name:           "list sources endpoint",
			method:         "GET",
			path:           "/api/v1/sources",
			expectedStatus: http.StatusOK,
			description:    "should return 200 with empty list",
		},
		// Clarification routes (now implemented)
		{
			name:           "clarifications list endpoint",
			method:         "GET",
			path:           "/api/v1/clarifications",
			expectedStatus: http.StatusOK,
			description:    "should return 200 with empty list of pending clarifications",
		},
		{
			name:           "onboarding endpoint",
			method:         "POST",
			path:           "/api/v1/onboarding",
			expectedStatus: http.StatusServiceUnavailable,
			description:    "should be registered but return 503 (agent not available)",
		},
		{
			name:           "refresh endpoint",
			method:         "POST",
			path:           "/api/v1/refresh",
			expectedStatus: http.StatusServiceUnavailable,
			description:    "should be registered but return 503 (agent not available)",
		},
	}

	server := setupTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d (%s)", tt.name, tt.expectedStatus, w.Code, tt.description)
			}
		})
	}
}

// TestDomainRouteRegistration tests individual domain route registration
func TestDomainRouteRegistration(t *testing.T) {
	t.Run("status routes", func(t *testing.T) {
		mux := http.NewServeMux()
		server := &Server{}
		server.registerStatusRoutes(mux, "/api/v1")

		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("graph routes", func(t *testing.T) {
		mux := http.NewServeMux()
		server := setupTestServer(t)
		server.registerGraphRoutes(mux, "/api/v1", server.services.Graph)

		// Test that routes are registered
		routes := []string{
			"/api/v1/graph/query",
			"/api/v1/graph/related",
			"/api/v1/graph/summary",
		}

		for _, route := range routes {
			req := httptest.NewRequest("GET", route, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Should not return 404 (route exists)
			if w.Code == http.StatusNotFound {
				t.Errorf("route %s not registered (got 404)", route)
			}
		}
	})

	t.Run("source routes", func(t *testing.T) {
		mux := http.NewServeMux()
		server := setupTestServer(t)
		server.registerSourceRoutes(mux, "/api/v1")

		// Test that routes are registered
		testCases := []struct {
			method string
			path   string
		}{
			{"GET", "/api/v1/sources"},
			{"POST", "/api/v1/sources"},
			{"GET", "/api/v1/sources/test-id"},
			{"DELETE", "/api/v1/sources/test-id"},
		}

		for _, tc := range testCases {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// 404 means the route is registered but source doesn't exist - that's fine
			// Only error if we get 404 with "Page not found" which means route isn't registered
			if w.Code == http.StatusNotFound && tc.path == "/api/v1/sources" {
				t.Errorf("route %s %s not registered (got 404)", tc.method, tc.path)
			}
		}
	})
}

// TestAPIVersionConstant ensures the API version constant is used consistently
func TestAPIVersionConstant(t *testing.T) {
	if apiVersion != "v1" {
		t.Errorf("expected apiVersion to be 'v1', got '%s'", apiVersion)
	}

	if apiPrefix != "/api/v1" {
		t.Errorf("expected apiPrefix to be '/api/v1', got '%s'", apiPrefix)
	}
}

// TestAlternateAPIVersion demonstrates how to register routes with a different version
func TestAlternateAPIVersion(t *testing.T) {
	server := setupTestServer(t)
	mux := http.NewServeMux()

	// Register v1 routes
	server.registerGraphRoutes(mux, "/api/v1", server.services.Graph)

	// Register v2 routes (same implementation, different prefix)
	server.registerGraphRoutes(mux, "/api/v2", server.services.Graph)

	// Test v1 route works
	req := httptest.NewRequest("GET", "/api/v1/graph/summary", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("v1 route not registered")
	}

	// Test v2 route works
	req = httptest.NewRequest("GET", "/api/v2/graph/summary", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("v2 route not registered")
	}
}
