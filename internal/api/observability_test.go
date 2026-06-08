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
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
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

func (m *mockPrometheusAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockPrometheusAdapter) Disconnect() error                                  { return nil }
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

func (m *mockLokiAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockLokiAdapter) Disconnect() error                                  { return nil }
func (m *mockLokiAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockLokiAdapter) Query(_ context.Context, _ string, _ int, _ time.Duration) (*lokiadapter.QueryResult, error) {
	return m.result, m.err
}
func (m *mockLokiAdapter) QueryRange(_ context.Context, _ string, _, _ time.Time, _ int) (*lokiadapter.QueryResult, error) {
	return m.result, m.err
}
func (m *mockLokiAdapter) ListServices(_ context.Context) ([]string, error) { return nil, nil }

// --- Tempo mock ---

type mockTempoAdapter struct {
	searchResults []tempoadapter.TraceSearchResult
	trace         *tempoadapter.Trace
	err           error
}

func (m *mockTempoAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockTempoAdapter) Disconnect() error                                  { return nil }
func (m *mockTempoAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockTempoAdapter) Search(_ context.Context, _, _ string, _, _, _ int) ([]tempoadapter.TraceSearchResult, error) {
	return m.searchResults, m.err
}
func (m *mockTempoAdapter) GetTrace(_ context.Context, _ string) (*tempoadapter.Trace, error) {
	return m.trace, m.err
}
func (m *mockTempoAdapter) ListServices(_ context.Context) ([]string, error) { return nil, nil }

// --- Jaeger mock ---

type mockJaegerAdapter struct {
	jaegerSvcs []string
	traces     []jaegeradapter.TraceSearchResult
	trace      map[string]any
	err        error
}

func (m *mockJaegerAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockJaegerAdapter) Disconnect() error                                  { return nil }
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

func TestHandlePrometheusQuery_MissingComponent(t *testing.T) {
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

func TestHandleLokiQuery_MissingComponent(t *testing.T) {
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

func TestHandleTempoSearch_MissingComponent(t *testing.T) {
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

func TestHandleJaegerServices_MissingComponent(t *testing.T) {
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

// --- Datadog mock ---

type mockDatadogAdapter struct {
	metricsResult *datadogadapter.MetricsResult
	logsResult    *datadogadapter.LogsResult
	err           error
}

func (m *mockDatadogAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockDatadogAdapter) Disconnect() error                                  { return nil }
func (m *mockDatadogAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockDatadogAdapter) MetricsQuery(_ context.Context, _ string, _, _ int64) (*datadogadapter.MetricsResult, error) {
	return m.metricsResult, m.err
}
func (m *mockDatadogAdapter) LogsSearch(_ context.Context, _ string, _, _ int64, _ int) (*datadogadapter.LogsResult, error) {
	return m.logsResult, m.err
}
func (m *mockDatadogAdapter) ListActiveServices(_ context.Context) ([]string, error) { return nil, nil }
func (m *mockDatadogAdapter) ListLogServices(_ context.Context) ([]string, error)    { return nil, nil }

// --- Splunk mock ---

type mockSplunkAdapter struct {
	result *splunkadapter.SearchResult
	err    error
}

func (m *mockSplunkAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockSplunkAdapter) Disconnect() error                                  { return nil }
func (m *mockSplunkAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockSplunkAdapter) Search(_ context.Context, _, _, _ string, _ int) (*splunkadapter.SearchResult, error) {
	return m.result, m.err
}

// --- Dynatrace mock ---

type mockDynatraceAdapter struct {
	metricsResult *dynatraceadapter.MetricsResult
	eventsResult  *dynatraceadapter.EventsResult
	err           error
}

func (m *mockDynatraceAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockDynatraceAdapter) Disconnect() error                                  { return nil }
func (m *mockDynatraceAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockDynatraceAdapter) MetricsQuery(_ context.Context, _ string, _, _ int64) (*dynatraceadapter.MetricsResult, error) {
	return m.metricsResult, m.err
}
func (m *mockDynatraceAdapter) Events(_ context.Context, _, _ int64, _ int) (*dynatraceadapter.EventsResult, error) {
	return m.eventsResult, m.err
}

// --- New Relic mock ---

type mockNewRelicAdapter struct {
	result *newrelicadapter.NRQLResult
	err    error
}

func (m *mockNewRelicAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (m *mockNewRelicAdapter) Disconnect() error                                  { return nil }
func (m *mockNewRelicAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}
func (m *mockNewRelicAdapter) NRQLQuery(_ context.Context, _ int, _ string) (*newrelicadapter.NRQLResult, error) {
	return m.result, m.err
}

// --- Setup helpers ---

func setupDatadogTestServer(t *testing.T, mock *mockDatadogAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-dd", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupSplunkTestServer(t *testing.T, mock *mockSplunkAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-splunk", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupDynatraceTestServer(t *testing.T, mock *mockDynatraceAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-dt", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func setupNewRelicTestServer(t *testing.T, mock *mockNewRelicAdapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register("test-nr", mock)
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// --- Datadog tests ---

func TestHandleDatadogMetrics(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		metricsResult *datadogadapter.MetricsResult
		err           error
		wantStatus    int
	}{
		{
			name:          "success",
			url:           "/api/v1/datadog/test-dd/metrics?query=avg:system.cpu.user{*}",
			metricsResult: &datadogadapter.MetricsResult{},
			wantStatus:    http.StatusOK,
		},
		{
			name:          "with from and to params",
			url:           "/api/v1/datadog/test-dd/metrics?query=avg:system.cpu.user{*}&from=1609459200&to=1609462800",
			metricsResult: &datadogadapter.MetricsResult{},
			wantStatus:    http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/datadog/test-dd/metrics",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/datadog/test-dd/metrics?query=avg:system.cpu.user{*}",
			err:        fmt.Errorf("datadog error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatadogTestServer(t, &mockDatadogAdapter{metricsResult: tt.metricsResult, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleDatadogMetrics_MissingComponent(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/datadog/nonexistent/metrics?query=up", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDatadogLogs(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		logsResult *datadogadapter.LogsResult
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/datadog/test-dd/logs?query=service:payment",
			logsResult: &datadogadapter.LogsResult{Logs: []datadogadapter.LogEntry{{Message: "test log"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised to empty",
			url:        "/api/v1/datadog/test-dd/logs?query=service:payment",
			logsResult: nil,
			wantStatus: http.StatusOK,
		},
		{
			name:       "with time and limit params",
			url:        "/api/v1/datadog/test-dd/logs?query=service:payment&from=1609459200&to=1609462800&limit=50",
			logsResult: &datadogadapter.LogsResult{Logs: []datadogadapter.LogEntry{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/datadog/test-dd/logs",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/datadog/test-dd/logs?query=service:payment",
			err:        fmt.Errorf("datadog logs error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatadogTestServer(t, &mockDatadogAdapter{logsResult: tt.logsResult, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Splunk tests ---

func TestHandleSplunkSearch(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		result     *splunkadapter.SearchResult
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/splunk/test-splunk/search?query=index%3Dmain+error",
			result:     &splunkadapter.SearchResult{Events: []splunkadapter.SearchEvent{{Raw: "error occurred"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised to empty",
			url:        "/api/v1/splunk/test-splunk/search?query=index%3Dmain+error",
			result:     nil,
			wantStatus: http.StatusOK,
		},
		{
			name:       "with earliest latest and limit",
			url:        "/api/v1/splunk/test-splunk/search?query=index%3Dmain&earliest=-2h&latest=now&limit=200",
			result:     &splunkadapter.SearchResult{Events: []splunkadapter.SearchEvent{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/splunk/test-splunk/search",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/splunk/test-splunk/search?query=index%3Dmain",
			err:        fmt.Errorf("splunk error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupSplunkTestServer(t, &mockSplunkAdapter{result: tt.result, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleSplunkSearch_MissingComponent(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/splunk/nonexistent/search?query=up", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- Dynatrace tests ---

func TestHandleDynatraceMetrics(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		metricsResult *dynatraceadapter.MetricsResult
		err           error
		wantStatus    int
	}{
		{
			name:          "success",
			url:           "/api/v1/dynatrace/test-dt/metrics?query=builtin:host.cpu.usage",
			metricsResult: &dynatraceadapter.MetricsResult{Resolution: "1m"},
			wantStatus:    http.StatusOK,
		},
		{
			name:          "with from and to params",
			url:           "/api/v1/dynatrace/test-dt/metrics?query=builtin:host.cpu.usage&from=1609459200000&to=1609462800000",
			metricsResult: &dynatraceadapter.MetricsResult{},
			wantStatus:    http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/dynatrace/test-dt/metrics",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/dynatrace/test-dt/metrics?query=builtin:host.cpu.usage",
			err:        fmt.Errorf("dynatrace error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDynatraceTestServer(t, &mockDynatraceAdapter{metricsResult: tt.metricsResult, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleDynatraceMetrics_MissingComponent(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/dynatrace/nonexistent/metrics?query=up", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDynatraceEvents(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		eventsResult *dynatraceadapter.EventsResult
		err          error
		wantStatus   int
	}{
		{
			name: "success with events",
			url:  "/api/v1/dynatrace/test-dt/events",
			eventsResult: &dynatraceadapter.EventsResult{
				Events: []dynatraceadapter.DynatraceEvent{{Title: "CPU spike", Type: "PERFORMANCE_EVENT"}},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:         "nil result normalised to empty",
			url:          "/api/v1/dynatrace/test-dt/events",
			eventsResult: nil,
			wantStatus:   http.StatusOK,
		},
		{
			name:         "with time and limit params",
			url:          "/api/v1/dynatrace/test-dt/events?from=1609459200000&to=1609462800000&limit=20",
			eventsResult: &dynatraceadapter.EventsResult{Events: []dynatraceadapter.DynatraceEvent{}},
			wantStatus:   http.StatusOK,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/dynatrace/test-dt/events",
			err:        fmt.Errorf("dynatrace events error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDynatraceTestServer(t, &mockDynatraceAdapter{eventsResult: tt.eventsResult, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- New Relic tests ---

func TestHandleNewRelicNRQL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		result     *newrelicadapter.NRQLResult
		err        error
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/newrelic/test-nr/nrql?query=SELECT+count(*)+FROM+Transaction",
			result:     &newrelicadapter.NRQLResult{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "with account_id",
			url:        "/api/v1/newrelic/test-nr/nrql?query=SELECT+count(*)+FROM+Transaction&account_id=12345",
			result:     &newrelicadapter.NRQLResult{},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing query",
			url:        "/api/v1/newrelic/test-nr/nrql",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/newrelic/test-nr/nrql?query=SELECT+count(*)+FROM+Transaction",
			err:        fmt.Errorf("newrelic error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupNewRelicTestServer(t, &mockNewRelicAdapter{result: tt.result, err: tt.err})

			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleNewRelicNRQL_MissingComponent(t *testing.T) {
	registry := adapters.NewRegistry()
	services := &core.Services{Config: &config.Config{}, Adapters: registry}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/newrelic/nonexistent/nrql?query=SELECT+1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}
