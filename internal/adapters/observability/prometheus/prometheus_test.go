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
	source := store.Source{
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
	source := store.Source{
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
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
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
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
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
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
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

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
