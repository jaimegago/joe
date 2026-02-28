package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- Falco mock ---

type mockFalcoAdapter struct {
	events []falcoadapter.Event
	rules  []falcoadapter.Rule
	err    error
}

func (m *mockFalcoAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockFalcoAdapter) Disconnect() error                               { return nil }
func (m *mockFalcoAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockFalcoAdapter) ListEvents(_ context.Context, _, _, _ string, _ int) ([]falcoadapter.Event, error) {
	return m.events, m.err
}
func (m *mockFalcoAdapter) ListRules(_ context.Context) ([]falcoadapter.Rule, error) {
	return m.rules, m.err
}

func setupSecurityServer(t *testing.T, sourceID string, mock adapters.Adapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register(sourceID, mock)
	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func TestHandleFalcoEvents_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/falco/nonexistent/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleFalcoEvents(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockFalcoAdapter
		wantStatus int
	}{
		{
			name: "success with events",
			mock: &mockFalcoAdapter{events: []falcoadapter.Event{
				{Rule: "Write below root", Priority: "warning"},
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "success empty list",
			mock:       &mockFalcoAdapter{events: []falcoadapter.Event{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockFalcoAdapter{err: fmt.Errorf("falco unavailable")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupSecurityServer(t, "falco-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/falco/falco-src/events", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleFalcoRules_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/falco/nonexistent/rules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleFalcoRules(t *testing.T) {
	mock := &mockFalcoAdapter{rules: []falcoadapter.Rule{{Name: "Write below root"}}}
	mux := setupSecurityServer(t, "falco-src", mock)
	req := httptest.NewRequest("GET", "/api/v1/falco/falco-src/rules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
