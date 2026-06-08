package envoy_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/networking/envoy"
	"github.com/jaimegago/joe/internal/store"
)

// mockHTTP implements httpDoer for tests.
type mockHTTP struct {
	responses map[string]mockResponse
	err       error
}

type mockResponse struct {
	status int
	body   string
}

func (m *mockHTTP) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := req.URL.Path
	if req.URL.RawQuery != "" {
		key += "?" + req.URL.RawQuery
	}
	mr, ok := m.responses[key]
	if !ok {
		// Default: 200 OK with empty JSON object.
		mr = mockResponse{status: 200, body: "{}"}
	}
	return &http.Response{
		StatusCode: mr.status,
		Body:       io.NopCloser(strings.NewReader(mr.body)),
	}, nil
}

func clustersJSON(clusters []map[string]any) string {
	b, _ := json.Marshal(map[string]any{"cluster_statuses": clusters})
	return string(b)
}

func clusterEntry(name string, hosts []map[string]any) map[string]any {
	return map[string]any{
		"name":          name,
		"host_statuses": hosts,
	}
}

func hostEntry(addr string, port int, status string, weight int) map[string]any {
	return map[string]any{
		"address": map[string]any{
			"socket_address": map[string]any{
				"address":    addr,
				"port_value": port,
			},
		},
		"health_status": map[string]any{
			"eds_health_status": status,
		},
		"weight": weight,
	}
}

func TestAdapter_Status_NotConnected(t *testing.T) {
	a := envoy.New()
	if a.Status().Connected {
		t.Error("expected not connected")
	}
}

func TestAdapter_Clusters(t *testing.T) {
	body := clustersJSON([]map[string]any{
		clusterEntry("outbound|80||backend.default.svc", []map[string]any{
			hostEntry("10.0.0.1", 80, "HEALTHY", 1),
			hostEntry("10.0.0.2", 80, "UNHEALTHY", 1),
		}),
		clusterEntry("outbound|443||api.default.svc", []map[string]any{
			hostEntry("10.0.1.1", 443, "HEALTHY", 2),
		}),
	})

	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/clusters?format=json": {status: 200, body: body},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})

	clusters, err := a.Clusters(context.Background())
	if err != nil {
		t.Fatalf("Clusters() error = %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
	if clusters[0].Name != "outbound|80||backend.default.svc" {
		t.Errorf("Name = %q", clusters[0].Name)
	}
	if len(clusters[0].HostStatuses) != 2 {
		t.Errorf("host count = %d, want 2", len(clusters[0].HostStatuses))
	}
	if clusters[0].HostStatuses[0].HealthStatus != "HEALTHY" {
		t.Errorf("health = %q, want HEALTHY", clusters[0].HostStatuses[0].HealthStatus)
	}
	if clusters[0].HostStatuses[0].Address != "10.0.0.1:80" {
		t.Errorf("address = %q, want 10.0.0.1:80", clusters[0].HostStatuses[0].Address)
	}
}

func TestAdapter_ConfigDump(t *testing.T) {
	dumpBody := `{"configs":[{"@type":"routes","dynamic_route_configs":[]}]}`
	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/config_dump": {status: 200, body: dumpBody},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})

	dump, err := a.ConfigDump(context.Background(), "")
	if err != nil {
		t.Fatalf("ConfigDump() error = %v", err)
	}
	if dump["configs"] == nil {
		t.Error("expected configs key in dump")
	}
}

func TestAdapter_ConfigDump_WithSection(t *testing.T) {
	dumpBody := `{"configs":[]}`
	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/config_dump?resource=routes": {status: 200, body: dumpBody},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})

	_, err := a.ConfigDump(context.Background(), "routes")
	if err != nil {
		t.Fatalf("ConfigDump(routes) error = %v", err)
	}
}

func TestAdapter_Stats(t *testing.T) {
	statsBody := `{"stats":[{"name":"cluster.backend.upstream_cx_active","value":5},{"name":"http.ingress.rq_total","value":1234}]}`
	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/stats?format=json": {status: 200, body: statsBody},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})

	stats, err := a.Stats(context.Background(), "")
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(stats))
	}
	if stats[0].Name != "cluster.backend.upstream_cx_active" {
		t.Errorf("Name = %q", stats[0].Name)
	}
}

func TestAdapter_Stats_WithFilter(t *testing.T) {
	statsBody := `{"stats":[{"name":"cluster.backend.upstream_cx_active","value":5}]}`
	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/stats?format=json&filter=cluster.backend": {status: 200, body: statsBody},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})

	stats, err := a.Stats(context.Background(), "cluster.backend")
	if err != nil {
		t.Fatalf("Stats(filter) error = %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	a := envoy.New()
	ctx := context.Background()

	if _, err := a.Clusters(ctx); !errors.Is(err, envoy.ErrNotConnected) {
		t.Errorf("Clusters: expected ErrNotConnected, got %v", err)
	}
	if _, err := a.ConfigDump(ctx, ""); !errors.Is(err, envoy.ErrNotConnected) {
		t.Errorf("ConfigDump: expected ErrNotConnected, got %v", err)
	}
	if _, err := a.Stats(ctx, ""); !errors.Is(err, envoy.ErrNotConnected) {
		t.Errorf("Stats: expected ErrNotConnected, got %v", err)
	}
}

func TestAdapter_HTTPError(t *testing.T) {
	cli := &mockHTTP{err: errors.New("connection refused")}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})

	if _, err := a.Clusters(context.Background()); err == nil {
		t.Error("expected error for http failure")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	a := envoy.NewWithClient(&mockHTTP{}, envoy.Config{URL: "http://envoy:9901"})
	if err := a.Disconnect(); err != nil {
		t.Errorf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected not connected after Disconnect")
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		wantURL string
	}{
		{
			name:    "valid config",
			raw:     `{"url":"http://envoy:9901","timeout_ms":5000}`,
			wantURL: "http://envoy:9901",
		},
		{
			name:    "missing url",
			raw:     `{"timeout_ms":5000}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			raw:     `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := envoy.ParseConfig([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", cfg.URL, tt.wantURL)
			}
		})
	}
}

func TestParseConfig_DefaultTimeout(t *testing.T) {
	// Test that default timeout is applied when timeout_ms is 0.
	cfg, err := envoy.ParseConfig([]byte(`{"url":"http://envoy:9901"}`))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.TimeoutMS != 10000 {
		t.Errorf("default TimeoutMS = %d, want 10000", cfg.TimeoutMS)
	}
}

func TestAdapter_ConfigDump_Error(t *testing.T) {
	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/config_dump": {status: 500, body: "internal error"},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})
	_, err := a.ConfigDump(context.Background(), "")
	if err == nil {
		t.Error("expected error for 500, got nil")
	}
}

func TestAdapter_Stats_Error(t *testing.T) {
	cli := &mockHTTP{
		responses: map[string]mockResponse{
			"/stats?format=json": {status: 503, body: "service unavailable"},
		},
	}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})
	_, err := a.Stats(context.Background(), "")
	if err == nil {
		t.Error("expected error for 503, got nil")
	}
}

func TestAdapter_Get_NetworkError(t *testing.T) {
	cli := &mockHTTP{err: errors.New("connection refused")}
	a := envoy.NewWithClient(cli, envoy.Config{URL: "http://envoy:9901"})
	_, err := a.Clusters(context.Background())
	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestAdapter_Status_Connected(t *testing.T) {
	a := envoy.NewWithClient(&mockHTTP{}, envoy.Config{URL: "http://envoy:9901"})
	s := a.Status()
	if !s.Connected {
		t.Error("expected connected status")
	}
	if s.Message == "" {
		t.Error("expected non-empty status message")
	}
}

func TestConnect_ViaComponent(t *testing.T) {
	// Test Connect via httptest server.
	mux := http.NewServeMux()
	mux.HandleFunc("/server_info", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version":"1.28.0"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := envoy.New()
	src := store.Component{
		Config: []byte(`{"url":"` + srv.URL + `"}`),
	}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected after Connect()")
	}
}

func TestConnect_BadStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/server_info", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := envoy.New()
	src := store.Component{Config: []byte(`{"url":"` + srv.URL + `"}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for 503 ping response")
	}
}

func TestConnect_BadConfig(t *testing.T) {
	a := envoy.New()
	src := store.Component{Config: []byte(`{}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for missing url")
	}
}
