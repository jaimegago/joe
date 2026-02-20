package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- Prometheus mock ---

type mockPrometheusAdapter struct {
	result  *prometheusadapter.QueryResult
	targets []prometheusadapter.Target
	err     error
}

func (m *mockPrometheusAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockPrometheusAdapter) Disconnect() error                                { return nil }
func (m *mockPrometheusAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockPrometheusAdapter) Query(_ context.Context, _ string, _ time.Time) (*prometheusadapter.QueryResult, error) {
	return m.result, m.err
}
func (m *mockPrometheusAdapter) QueryRange(_ context.Context, _ string, _, _ time.Time, _ time.Duration) (*prometheusadapter.QueryResult, error) {
	return m.result, m.err
}
func (m *mockPrometheusAdapter) Targets(_ context.Context) ([]prometheusadapter.Target, error) {
	return m.targets, m.err
}

// --- Loki mock ---

type mockLokiAdapter struct {
	result *lokiadapter.QueryResult
	err    error
}

func (m *mockLokiAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockLokiAdapter) Disconnect() error                                { return nil }
func (m *mockLokiAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockLokiAdapter) Query(_ context.Context, _ string, _ int, _ time.Duration) (*lokiadapter.QueryResult, error) {
	return m.result, m.err
}
func (m *mockLokiAdapter) QueryRange(_ context.Context, _ string, _, _ time.Time, _ int) (*lokiadapter.QueryResult, error) {
	return m.result, m.err
}

// --- Tempo mock ---

type mockTempoAdapter struct {
	searchResults []tempoadapter.TraceSearchResult
	trace         *tempoadapter.Trace
	err           error
}

func (m *mockTempoAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockTempoAdapter) Disconnect() error                                { return nil }
func (m *mockTempoAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockTempoAdapter) Search(_ context.Context, _, _ string, _, _, _ int) ([]tempoadapter.TraceSearchResult, error) {
	return m.searchResults, m.err
}
func (m *mockTempoAdapter) GetTrace(_ context.Context, _ string) (*tempoadapter.Trace, error) {
	return m.trace, m.err
}

// --- Jaeger mock ---

type mockJaegerAdapter struct {
	jaegerSvcs []string
	traces     []jaegeradapter.TraceSearchResult
	trace      map[string]any
	err        error
}

func (m *mockJaegerAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockJaegerAdapter) Disconnect() error                                { return nil }
func (m *mockJaegerAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockJaegerAdapter) ListServices(_ context.Context) ([]string, error) {
	return m.jaegerSvcs, m.err
}
func (m *mockJaegerAdapter) SearchTraces(_ context.Context, _, _ string, _ int) ([]jaegeradapter.TraceSearchResult, error) {
	return m.traces, m.err
}
func (m *mockJaegerAdapter) GetTrace(_ context.Context, _ string) (map[string]any, error) {
	return m.trace, m.err
}

// --- Setup helpers ---

func setupPrometheusTestServer(t *testing.T, mock *mockPrometheusAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-prom", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupLokiTestServer(t *testing.T, mock *mockLokiAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-loki", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupTempoTestServer(t *testing.T, mock *mockTempoAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-tempo", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupJaegerTestServer(t *testing.T, mock *mockJaegerAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-jaeger", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// --- Prometheus tests ---

func TestHandlePrometheusQuery(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		result     *prometheusadapter.QueryResult
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/prometheus/test-prom/query?query=up",
			result:     &prometheusadapter.QueryResult{ResultType: "vector"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with explicit time",
			url:        "/api/v1/prometheus/test-prom/query?query=up&time=1609459200",
			result:     &prometheusadapter.QueryResult{ResultType: "vector"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/prometheus/test-prom/query",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid time",
			url:        "/api/v1/prometheus/test-prom/query?query=up&time=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/prometheus/test-prom/query?query=up",
			err:        fmt.Errorf("prometheus error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupPrometheusTestServer(t, &mockPrometheusAdapter{result: tt.result, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandlePrometheusQuery_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/prometheus/nonexistent/query?query=up", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePrometheusQueryRange(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		result     *prometheusadapter.QueryResult
		err        error
		wantStatus int
	}{
		{
			name:       "success with step",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=1609459200&end=1609462800&step=60",
			result:     &prometheusadapter.QueryResult{ResultType: "matrix"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "success default step",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=1609459200&end=1609462800",
			result:     &prometheusadapter.QueryResult{ResultType: "matrix"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/prometheus/test-prom/query_range?start=1609459200&end=1609462800",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid start",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=invalid&end=1609462800",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid end",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=1609459200&end=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid step non-numeric",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=1609459200&end=1609462800&step=bad",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid step zero",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=1609459200&end=1609462800&step=0",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/prometheus/test-prom/query_range?query=up&start=1609459200&end=1609462800",
			err:        fmt.Errorf("prometheus error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupPrometheusTestServer(t, &mockPrometheusAdapter{result: tt.result, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandlePrometheusTargets(t *testing.T) {
	tests := []struct {
		name       string
		targets    []prometheusadapter.Target
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "success with targets",
			targets: []prometheusadapter.Target{
				{Labels: map[string]string{"job": "node"}, State: "active"},
				{Labels: map[string]string{"job": "kubelet"}, State: "active"},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "nil targets normalised to empty",
			targets:    nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("prometheus error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupPrometheusTestServer(t, &mockPrometheusAdapter{targets: tt.targets, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/prometheus/test-prom/targets", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

// --- Loki tests ---

func TestHandleLokiQuery(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		result     *lokiadapter.QueryResult
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/loki/test-loki/query?query=%7Bapp%3D%22payment%22%7D",
			result:     &lokiadapter.QueryResult{ResultType: "streams"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with limit and since params",
			url:        "/api/v1/loki/test-loki/query?query=%7Bapp%3D%22payment%22%7D&limit=50&since=3600",
			result:     &lokiadapter.QueryResult{ResultType: "streams"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/loki/test-loki/query",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/loki/test-loki/query?query=%7Bapp%3D%22payment%22%7D",
			err:        fmt.Errorf("loki error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupLokiTestServer(t, &mockLokiAdapter{result: tt.result, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleLokiQuery_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/loki/nonexistent/query?query=up", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleLokiQueryRange(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		result     *lokiadapter.QueryResult
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/loki/test-loki/query_range?query=%7Bapp%3D%22payment%22%7D&start=1609459200&end=1609462800",
			result:     &lokiadapter.QueryResult{ResultType: "streams"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with custom limit",
			url:        "/api/v1/loki/test-loki/query_range?query=%7Bapp%3D%22payment%22%7D&start=1609459200&end=1609462800&limit=200",
			result:     &lokiadapter.QueryResult{ResultType: "streams"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/loki/test-loki/query_range?start=1609459200&end=1609462800",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid start",
			url:        "/api/v1/loki/test-loki/query_range?query=up&start=invalid&end=1609462800",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid end",
			url:        "/api/v1/loki/test-loki/query_range?query=up&start=1609459200&end=invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/loki/test-loki/query_range?query=%7Bapp%3D%22payment%22%7D&start=1609459200&end=1609462800",
			err:        fmt.Errorf("loki error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupLokiTestServer(t, &mockLokiAdapter{result: tt.result, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Tempo tests ---

func TestHandleTempoSearch(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		results    []tempoadapter.TraceSearchResult
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name:       "success with results",
			url:        "/api/v1/tempo/test-tempo/search?service=payment",
			results:    []tempoadapter.TraceSearchResult{{TraceID: "abc123", RootServiceName: "payment"}},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "nil results normalised to empty",
			url:        "/api/v1/tempo/test-tempo/search",
			results:    nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "with duration params",
			url:        "/api/v1/tempo/test-tempo/search?service=payment&min_duration=100&max_duration=5000&limit=10",
			results:    []tempoadapter.TraceSearchResult{},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/tempo/test-tempo/search?service=payment",
			err:        fmt.Errorf("tempo error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupTempoTestServer(t, &mockTempoAdapter{searchResults: tt.results, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleTempoSearch_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/tempo/nonexistent/search", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleTempoGetTrace(t *testing.T) {
	tests := []struct {
		name       string
		traceID    string
		trace      *tempoadapter.Trace
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			traceID:    "abc123",
			trace:      &tempoadapter.Trace{TraceID: "abc123", SpanCount: 5},
			wantStatus: http.StatusOK,
		},
		{
			name:       "trace not found",
			traceID:    "notexist",
			err:        tempoadapter.ErrTraceNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "adapter error",
			traceID:    "abc123",
			err:        fmt.Errorf("tempo error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupTempoTestServer(t, &mockTempoAdapter{trace: tt.trace, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/tempo/test-tempo/traces/"+tt.traceID, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Jaeger tests ---

func TestHandleJaegerServices(t *testing.T) {
	tests := []struct {
		name       string
		services   []string
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name:       "success with services",
			services:   []string{"payment", "order", "user"},
			wantStatus: http.StatusOK,
			wantCount:  3,
		},
		{
			name:       "nil services normalised to empty",
			services:   nil,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("jaeger error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupJaegerTestServer(t, &mockJaegerAdapter{jaegerSvcs: tt.services, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/jaeger/test-jaeger/services", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleJaegerServices_MissingSource(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/jaeger/nonexistent/services", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleJaegerTraces(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		traces     []jaegeradapter.TraceSearchResult
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name:       "success",
			url:        "/api/v1/jaeger/test-jaeger/traces?service=payment",
			traces:     []jaegeradapter.TraceSearchResult{{TraceID: "abc123", Service: "payment"}},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "with operation and limit",
			url:        "/api/v1/jaeger/test-jaeger/traces?service=payment&operation=GET&limit=5",
			traces:     []jaegeradapter.TraceSearchResult{},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "missing service",
			url:        "/api/v1/jaeger/test-jaeger/traces",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/jaeger/test-jaeger/traces?service=payment",
			err:        fmt.Errorf("jaeger error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupJaegerTestServer(t, &mockJaegerAdapter{traces: tt.traces, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleJaegerGetTrace(t *testing.T) {
	tests := []struct {
		name       string
		traceID    string
		trace      map[string]any
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			traceID:    "abc123",
			trace:      map[string]any{"traceID": "abc123", "spans": []any{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "trace not found",
			traceID:    "notexist",
			err:        jaegeradapter.ErrTraceNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "adapter error",
			traceID:    "abc123",
			err:        fmt.Errorf("jaeger error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupJaegerTestServer(t, &mockJaegerAdapter{trace: tt.trace, err: tt.err})

			req := httptest.NewRequest("GET", "/api/v1/jaeger/test-jaeger/traces/"+tt.traceID, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
