package prometheus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	"github.com/jaimegago/joe/internal/store"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]any
		wantURL   string
		wantOrgID string
		wantErr   bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"url": "http://prometheus:9090"},
			wantURL: "http://prometheus:9090",
		},
		{
			name:      "with org_id",
			config:    map[string]any{"url": "http://mimir:9090", "org_id": "my-tenant"},
			wantURL:   "http://mimir:9090",
			wantOrgID: "my-tenant",
		},
		{
			name:    "missing url",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := prometheus.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if cfg.URL != tt.wantURL {
					t.Errorf("URL = %q, want %q", cfg.URL, tt.wantURL)
				}
				if cfg.OrgID != tt.wantOrgID {
					t.Errorf("OrgID = %q, want %q", cfg.OrgID, tt.wantOrgID)
				}
			}
		})
	}
}

func TestAdapter_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status/buildinfo" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter := prometheus.New()
	source := store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}

	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	status := adapter.Status()
	if !status.Connected {
		t.Errorf("Status().Connected = false, want true")
	}
}

func TestAdapter_Connect_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	adapter := prometheus.New()
	source := store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_Query(t *testing.T) {
	vectorResp := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"__name__": "up", "job": "my-service"},
					"value": [1234567890.123, "1"]
				}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/query":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(vectorResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := prometheus.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	result, err := adapter.Query(context.Background(), "up", time.Now())
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if result.ResultType != "vector" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "vector")
	}
	if len(result.Vector) != 1 {
		t.Errorf("len(Vector) = %d, want 1", len(result.Vector))
	}
	if result.Vector[0].Metric["job"] != "my-service" {
		t.Errorf("job label = %q, want %q", result.Vector[0].Metric["job"], "my-service")
	}
}

func TestAdapter_QueryRange(t *testing.T) {
	matrixResp := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [
				{
					"metric": {"job": "payment"},
					"values": [[1234567890, "0.5"], [1234567905, "0.6"]]
				}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/query_range":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(matrixResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := prometheus.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	now := time.Now()
	result, err := adapter.QueryRange(context.Background(), "rate(http_requests_total[5m])",
		now.Add(-5*time.Minute), now, 15*time.Second)
	if err != nil {
		t.Fatalf("QueryRange() error = %v", err)
	}

	if result.ResultType != "matrix" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "matrix")
	}
	if len(result.Matrix) != 1 {
		t.Errorf("len(Matrix) = %d, want 1", len(result.Matrix))
	}
}

func TestAdapter_Targets(t *testing.T) {
	targetsResp := `{
		"status": "success",
		"data": {
			"activeTargets": [
				{
					"labels": {"job": "payment", "instance": "payment:8080"},
					"scrapeUrl": "http://payment:8080/metrics",
					"lastError": "",
					"lastScrape": "2024-01-01T00:00:00Z"
				}
			],
			"droppedTargets": []
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/targets":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(targetsResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter := prometheus.New()
	source := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	targets, err := adapter.Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}

	if len(targets) != 1 {
		t.Errorf("len(targets) = %d, want 1", len(targets))
	}
	if targets[0].State != "active" {
		t.Errorf("State = %q, want %q", targets[0].State, "active")
	}
	if targets[0].Labels["job"] != "payment" {
		t.Errorf("job label = %q, want %q", targets[0].Labels["job"], "payment")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	adapter := prometheus.NewWithClient(&http.Client{})
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after disconnect, want false")
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	adapter := prometheus.New()
	_, err := adapter.Query(context.Background(), "up", time.Now())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

// ---------------------------------------------------------------------------
// ParseConfig additional coverage
// ---------------------------------------------------------------------------

func TestParseConfig_WithAPIKey(t *testing.T) {
	cfg, err := prometheus.ParseConfig(map[string]any{
		"url":     "http://prometheus:9090",
		"api_key": "secret-token",
	})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.APIKey != "secret-token" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "secret-token")
	}
}

// ---------------------------------------------------------------------------
// Connect additional coverage
// ---------------------------------------------------------------------------

func TestAdapter_Connect_EmptyConfig(t *testing.T) {
	// Empty config (no bytes) → falls into else branch → make(map[string]any)
	// ParseConfig will fail because URL is required.
	adapter := prometheus.New()
	source := store.Component{Config: nil}
	err := adapter.Connect(context.Background(), source)
	if err == nil {
		t.Error("Connect() with empty config expected error, got nil")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := prometheus.New()
	source := store.Component{Config: []byte(`{bad json`)}
	err := adapter.Connect(context.Background(), source)
	if err == nil {
		t.Error("Connect() with bad JSON expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Query error-path coverage (via NewWithClient)
// ---------------------------------------------------------------------------

func TestAdapter_Query_HTTP500(t *testing.T) {
	buildinfoDone := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status/buildinfo" && !buildinfoDone {
			buildinfoDone = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := a.Query(context.Background(), "up", time.Now())
	if err == nil {
		t.Error("Query() expected error on HTTP 500, got nil")
	}
}

func TestAdapter_Query_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{bad json`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := a.Query(context.Background(), "up", time.Now())
	if err == nil {
		t.Error("Query() expected parse error, got nil")
	}
}

func TestAdapter_Query_ErrorStatus(t *testing.T) {
	errResp := `{"status":"error","errorType":"bad_data","error":"parse error at char 5"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(errResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := a.Query(context.Background(), "up{}", time.Now())
	if err == nil {
		t.Error("Query() expected error for status=error response, got nil")
	}
}

// ---------------------------------------------------------------------------
// QueryRange additional coverage
// ---------------------------------------------------------------------------

func TestAdapter_QueryRange_SmallStep(t *testing.T) {
	// step < 1 second → stepSec defaults to 15
	matrixResp := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [
				{
					"metric": {"job": "svc"},
					"values": [[1234567890, "1"]]
				}
			]
		}
	}`

	var capturedStep string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/query_range":
			capturedStep = r.URL.Query().Get("step")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(matrixResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	now := time.Now()
	// Pass step = 0 (< 1 second) to trigger the default-to-15 branch.
	result, err := a.QueryRange(context.Background(), "up", now.Add(-5*time.Minute), now, 0)
	if err != nil {
		t.Fatalf("QueryRange() error = %v", err)
	}
	if result.ResultType != "matrix" {
		t.Errorf("ResultType = %q, want matrix", result.ResultType)
	}
	if capturedStep != "15" {
		t.Errorf("step param = %q, want %q", capturedStep, "15")
	}
}

// ---------------------------------------------------------------------------
// Targets error-path coverage
// ---------------------------------------------------------------------------

func TestAdapter_Targets_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/targets":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := a.Targets(context.Background())
	if err == nil {
		t.Error("Targets() expected error on HTTP 500, got nil")
	}
}

func TestAdapter_Targets_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/targets":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{bad json`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := a.Targets(context.Background())
	if err == nil {
		t.Error("Targets() expected parse error, got nil")
	}
}

func TestAdapter_Targets_WithDroppedTargets(t *testing.T) {
	targetsResp := `{
		"status": "success",
		"data": {
			"activeTargets": [
				{
					"labels": {"job": "api", "instance": "api:8080"},
					"scrapeUrl": "http://api:8080/metrics",
					"lastError": "",
					"lastScrape": "2024-01-01T00:00:00Z"
				}
			],
			"droppedTargets": [
				{
					"discoveredLabels": {"job": "legacy", "__address__": "old-host:9090"}
				}
			]
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case "/api/v1/targets":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(targetsResp))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": srv.URL}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	targets, err := a.Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}

	// First target should be active.
	if targets[0].State != "active" {
		t.Errorf("targets[0].State = %q, want %q", targets[0].State, "active")
	}
	if targets[0].Labels["job"] != "api" {
		t.Errorf("targets[0] job label = %q, want %q", targets[0].Labels["job"], "api")
	}

	// Second target should be dropped.
	if targets[1].State != "dropped" {
		t.Errorf("targets[1].State = %q, want %q", targets[1].State, "dropped")
	}
	if targets[1].Labels["job"] != "legacy" {
		t.Errorf("targets[1] job label = %q, want %q", targets[1].Labels["job"], "legacy")
	}
}

// ---------------------------------------------------------------------------
// QueryRange / Targets not-connected coverage
// ---------------------------------------------------------------------------

func TestAdapter_QueryRange_NotConnected(t *testing.T) {
	adapter := prometheus.New()
	_, err := adapter.QueryRange(context.Background(), "up", time.Now().Add(-time.Minute), time.Now(), 15*time.Second)
	if err == nil {
		t.Error("QueryRange() expected error when not connected, got nil")
	}
}

func TestAdapter_Targets_NotConnected(t *testing.T) {
	adapter := prometheus.New()
	_, err := adapter.Targets(context.Background())
	if err == nil {
		t.Error("Targets() expected error when not connected, got nil")
	}
}

// TestAdapter_Connect_NetworkError covers the client.Do failure path in Connect.
func TestAdapter_Connect_NetworkError(t *testing.T) {
	// Start a server then immediately close it so the TCP connection is refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	adapter := prometheus.New()
	err := adapter.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": addr}),
	})
	if err == nil {
		t.Error("Connect() expected network error, got nil")
	}
}

// TestAdapter_Targets_NetworkError covers the client.Do failure path in Targets.
func TestAdapter_Targets_NetworkError(t *testing.T) {
	connectDone := false
	var targetAddr string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status/buildinfo" && !connectDone {
			connectDone = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		}
	}))
	targetAddr = srv.URL

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{"url": targetAddr}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Close the server so the Targets call gets a connection refused error.
	srv.Close()

	_, err := a.Targets(context.Background())
	if err == nil {
		t.Error("Targets() expected network error after server closed, got nil")
	}
}

// ---------------------------------------------------------------------------
// addHeaders coverage — verify Authorization and X-Scope-OrgID are sent
// ---------------------------------------------------------------------------

func TestAdapter_addHeaders_WithAPIKey(t *testing.T) {
	var gotAuth, gotOrgID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			gotAuth = r.Header.Get("Authorization")
			gotOrgID = r.Header.Get("X-Scope-OrgID")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := prometheus.New()
	if err := a.Connect(context.Background(), store.Component{
		Config: mustMarshal(t, map[string]any{
			"url":     srv.URL,
			"api_key": "my-token",
			"org_id":  "tenant-42",
		}),
	}); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if gotAuth != "Bearer my-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer my-token")
	}
	if gotOrgID != "tenant-42" {
		t.Errorf("X-Scope-OrgID header = %q, want %q", gotOrgID, "tenant-42")
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
