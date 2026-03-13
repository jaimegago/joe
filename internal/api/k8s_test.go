package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/test/mocks"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	_ "modernc.org/sqlite"
)

func setupK8sTestServer(t *testing.T, mock *mocks.MockK8sAdapter) (*api.Server, *http.ServeMux) {
	t.Helper()

	registry := adapters.NewRegistry()
	registry.Register("test-cluster", mock)

	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}

	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return server, mux
}

func TestHandleK8sListResources(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		query      string
		mock       *mocks.MockK8sAdapter
		wantStatus int
		wantCount  int
	}{
		{
			name:     "list pods",
			sourceID: "test-cluster",
			query:    "resource=pods&namespace=default",
			mock: &mocks.MockK8sAdapter{
				ListResult: []unstructured.Unstructured{
					{Object: map[string]any{"metadata": map[string]any{"name": "pod-a"}}},
					{Object: map[string]any{"metadata": map[string]any{"name": "pod-b"}}},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:     "empty result",
			sourceID: "test-cluster",
			query:    "resource=pods&namespace=empty",
			mock: &mocks.MockK8sAdapter{
				ListResult: []unstructured.Unstructured{},
			},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "missing resource param",
			sourceID:   "test-cluster",
			query:      "namespace=default",
			mock:       mocks.NewMockK8sAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "source not found",
			sourceID:   "nonexistent",
			query:      "resource=pods",
			mock:       mocks.NewMockK8sAdapter(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:     "adapter error",
			sourceID: "test-cluster",
			query:    "resource=pods",
			mock: &mocks.MockK8sAdapter{
				ListErr: fmt.Errorf("connection refused"),
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupK8sTestServer(t, tt.mock)

			path := fmt.Sprintf("/api/v1/k8s/%s/resources?%s", tt.sourceID, tt.query)
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var body map[string]any
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				count := int(body["count"].(float64))
				if count != tt.wantCount {
					t.Errorf("count = %d, want %d", count, tt.wantCount)
				}
			}
		})
	}
}

func TestHandleK8sGetResource(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		resource   string
		namespace  string
		resName    string
		mock       *mocks.MockK8sAdapter
		wantStatus int
	}{
		{
			name:      "get existing pod",
			sourceID:  "test-cluster",
			resource:  "pods",
			namespace: "default",
			resName:   "web",
			mock: &mocks.MockK8sAdapter{
				GetResult: &unstructured.Unstructured{
					Object: map[string]any{
						"metadata": map[string]any{"name": "web", "namespace": "default"},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:      "adapter error",
			sourceID:  "test-cluster",
			resource:  "pods",
			namespace: "default",
			resName:   "missing",
			mock: &mocks.MockK8sAdapter{
				GetErr: fmt.Errorf("not found"),
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "source not found",
			sourceID:   "nonexistent",
			resource:   "pods",
			namespace:  "default",
			resName:    "web",
			mock:       mocks.NewMockK8sAdapter(),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupK8sTestServer(t, tt.mock)

			path := fmt.Sprintf("/api/v1/k8s/%s/resources/%s/%s/%s", tt.sourceID, tt.resource, tt.namespace, tt.resName)
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleK8sGetLogs(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   string
		namespace  string
		pod        string
		query      string
		mock       *mocks.MockK8sAdapter
		wantStatus int
		wantLogs   string
	}{
		{
			name:      "get logs",
			sourceID:  "test-cluster",
			namespace: "default",
			pod:       "web-abc",
			mock: &mocks.MockK8sAdapter{
				LogsResult: "2024-01-15 payment processed\n2024-01-15 request complete\n",
			},
			wantStatus: http.StatusOK,
			wantLogs:   "2024-01-15 payment processed\n2024-01-15 request complete\n",
		},
		{
			name:      "with container and tail",
			sourceID:  "test-cluster",
			namespace: "prod",
			pod:       "sidecar-pod",
			query:     "container=app&tail=50",
			mock: &mocks.MockK8sAdapter{
				LogsResult: "log line\n",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid tail",
			sourceID:   "test-cluster",
			namespace:  "default",
			pod:        "web",
			query:      "tail=abc",
			mock:       mocks.NewMockK8sAdapter(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "source not found",
			sourceID:   "nonexistent",
			namespace:  "default",
			pod:        "web",
			mock:       mocks.NewMockK8sAdapter(),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, mux := setupK8sTestServer(t, tt.mock)

			path := fmt.Sprintf("/api/v1/k8s/%s/logs/%s/%s", tt.sourceID, tt.namespace, tt.pod)
			if tt.query != "" {
				path += "?" + tt.query
			}
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK && tt.wantLogs != "" {
				var body map[string]any
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body["logs"] != tt.wantLogs {
					t.Errorf("logs = %v, want %v", body["logs"], tt.wantLogs)
				}
			}
		})
	}
}
