package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	onboardingErr    error
	refreshErr       error
	refreshSourceErr error
	onboardingCalled bool
	refreshCalled    bool
	refreshSourceID  string
	onboardingInput  string
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

func (m *mockCoreAgent) TriggerRefreshSource(ctx context.Context, sourceID string) error {
	m.refreshSourceID = sourceID
	return m.refreshSourceErr
}

func setupControlTestServer(t *testing.T, agent core.CoreAgent) *Server {
	t.Helper()

	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
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

func TestHandleOnboarding(t *testing.T) {
	tests := []struct {
		name           string
		payload        map[string]string
		agent          core.CoreAgent
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "success",
			payload:        map[string]string{"input": "we have a kubernetes cluster"},
			agent:          &mockCoreAgent{},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing input field",
			payload:        map[string]string{},
			agent:          &mockCoreAgent{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing required field 'input'",
		},
		{
			name:           "empty input value",
			payload:        map[string]string{"input": ""},
			agent:          &mockCoreAgent{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing required field 'input'",
		},
		{
			name:           "agent processing error",
			payload:        map[string]string{"input": "test input"},
			agent:          &mockCoreAgent{onboardingErr: fmt.Errorf("processing failed")},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal server error",
		},
		{
			name:           "agent unavailable",
			payload:        map[string]string{"input": "test"},
			agent:          nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedError:  "core agent not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupControlTestServer(t, tt.agent)
			mux := http.NewServeMux()
			server.RegisterRoutes(mux)

			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest("POST", "/api/v1/onboarding", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.expectedStatus)
			}

			if tt.expectedError != "" {
				var resp map[string]any
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if msg, ok := resp["message"].(string); !ok || msg != tt.expectedError {
					t.Errorf("message = %q, want %q", msg, tt.expectedError)
				}
			}

			if tt.expectedStatus == http.StatusOK && tt.agent != nil {
				mock := tt.agent.(*mockCoreAgent)
				if !mock.onboardingCalled {
					t.Error("ProcessOnboarding was not called")
				}
				if mock.onboardingInput != tt.payload["input"] {
					t.Errorf("input = %q, want %q", mock.onboardingInput, tt.payload["input"])
				}
			}
		})
	}
}

func TestHandleRefresh(t *testing.T) {
	tests := []struct {
		name           string
		payload        map[string]string
		agent          core.CoreAgent
		expectedStatus int
		expectedError  string
		checkSourceID  string
	}{
		{
			name:           "full refresh success",
			payload:        map[string]string{},
			agent:          &mockCoreAgent{},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "source refresh success",
			payload:        map[string]string{"source_id": "src-123"},
			agent:          &mockCoreAgent{},
			expectedStatus: http.StatusOK,
			checkSourceID:  "src-123",
		},
		{
			name:           "refresh error",
			payload:        map[string]string{},
			agent:          &mockCoreAgent{refreshErr: fmt.Errorf("refresh failed")},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal server error",
		},
		{
			name:           "source not found",
			payload:        map[string]string{"source_id": "nonexistent"},
			agent:          &mockCoreAgent{refreshSourceErr: fmt.Errorf("%w: nonexistent", store.ErrSourceNotFound)},
			expectedStatus: http.StatusNotFound,
			expectedError:  "source 'nonexistent' not found",
		},
		{
			name:           "agent unavailable",
			payload:        map[string]string{},
			agent:          nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedError:  "core agent not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupControlTestServer(t, tt.agent)
			mux := http.NewServeMux()
			server.RegisterRoutes(mux)

			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest("POST", "/api/v1/refresh", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.expectedStatus)
			}

			if tt.expectedError != "" {
				var resp map[string]any
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if msg, ok := resp["message"].(string); !ok || msg != tt.expectedError {
					t.Errorf("message = %q, want %q", msg, tt.expectedError)
				}
			}

			if tt.expectedStatus == http.StatusOK && tt.agent != nil {
				mock := tt.agent.(*mockCoreAgent)
				if tt.checkSourceID != "" {
					if mock.refreshSourceID != tt.checkSourceID {
						t.Errorf("refreshSourceID = %q, want %q", mock.refreshSourceID, tt.checkSourceID)
					}
				} else {
					if !mock.refreshCalled {
						t.Error("TriggerRefresh was not called")
					}
				}
			}
		})
	}
}

func TestHandleRefreshEmptyBody(t *testing.T) {
	// Test that empty body (no JSON) triggers full refresh
	agent := &mockCoreAgent{}
	server := setupControlTestServer(t, agent)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/refresh", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if !agent.refreshCalled {
		t.Error("TriggerRefresh was not called for empty body")
	}
}
