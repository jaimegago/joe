package elasticsearch_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	"github.com/jaimegago/joe/internal/store"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

// mockHTTP returns a fixed response for every request.
type mockHTTP struct {
	statusCode int
	body       string
}

func (m *mockHTTP) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
	}, nil
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
	}{
		{
			name:  "valid config",
			input: map[string]any{"url": "http://es:9200"},
		},
		{
			name:  "with credentials",
			input: map[string]any{"url": "http://es:9200", "username": "elastic", "password": "pass"},
		},
		{
			name:  "with api key",
			input: map[string]any{"url": "http://es:9200", "api_key": "mykey"},
		},
		{
			name:    "missing url",
			input:   map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := elasticsearch.ParseConfig(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.URL == "" {
				t.Error("URL should not be empty on valid config")
			}
		})
	}
}

func TestConnect_Success(t *testing.T) {
	healthResp := `{"cluster_name":"test","status":"green","active_shards":10,"unassigned_shards":0,"number_of_nodes":3}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(healthResp))
	}))
	defer srv.Close()

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("Status().Connected = false after successful connect")
	}
}

func TestConnect_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for 401, got nil")
	}
}

func TestConnect_InvalidConfig(t *testing.T) {
	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{})} // missing url
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for missing url, got nil")
	}
}

func TestConnect_BadJSON(t *testing.T) {
	a := elasticsearch.New()
	src := store.Component{Config: []byte(`{bad`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for bad JSON, got nil")
	}
}

func TestStatus_NotConnected(t *testing.T) {
	a := elasticsearch.New()
	if a.Status().Connected {
		t.Error("Status().Connected = true before connect, want false")
	}
}

func TestDisconnect(t *testing.T) {
	m := &mockHTTP{statusCode: http.StatusOK, body: "{}"}
	a := elasticsearch.NewWithClient(m)
	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("Status().Connected = true after disconnect, want false")
	}
}

func TestClusterHealth_NotConnected(t *testing.T) {
	a := elasticsearch.New()
	_, err := a.ClusterHealth(context.Background())
	if err == nil {
		t.Error("ClusterHealth() expected error when not connected, got nil")
	}
}

func TestClusterHealth_Success(t *testing.T) {
	body := `{"cluster_name":"my-cluster","status":"green","active_shards":20,"unassigned_shards":0,"number_of_nodes":5}`
	m := &mockHTTP{statusCode: http.StatusOK, body: body}
	a := elasticsearch.NewWithClient(m)

	health, err := a.ClusterHealth(context.Background())
	if err != nil {
		t.Fatalf("ClusterHealth() error = %v", err)
	}
	if health.ClusterName != "my-cluster" {
		t.Errorf("ClusterName = %q, want my-cluster", health.ClusterName)
	}
	if health.Status != "green" {
		t.Errorf("Status = %q, want green", health.Status)
	}
	if health.Nodes != 5 {
		t.Errorf("Nodes = %d, want 5", health.Nodes)
	}
	if health.Shards != 20 {
		t.Errorf("Shards = %d, want 20", health.Shards)
	}
}

func TestClusterHealth_ErrorStatus(t *testing.T) {
	m := &mockHTTP{statusCode: http.StatusServiceUnavailable, body: "service unavailable"}
	a := elasticsearch.NewWithClient(m)
	_, err := a.ClusterHealth(context.Background())
	if err == nil {
		t.Error("ClusterHealth() expected error for 503, got nil")
	}
}

func TestClusterHealth_InvalidJSON(t *testing.T) {
	m := &mockHTTP{statusCode: http.StatusOK, body: "not json"}
	a := elasticsearch.NewWithClient(m)
	_, err := a.ClusterHealth(context.Background())
	if err == nil {
		t.Error("ClusterHealth() expected error for invalid JSON, got nil")
	}
}

func TestListIndices_NotConnected(t *testing.T) {
	a := elasticsearch.New()
	_, err := a.ListIndices(context.Background(), "")
	if err == nil {
		t.Error("ListIndices() expected error when not connected, got nil")
	}
}

func TestListIndices_Success(t *testing.T) {
	body := `[
		{"index":"orders-2024","status":"open","health":"green","docs.count":"10000","store.size":"50mb","pri":"1","rep":"1"},
		{"index":"logs-2024","status":"open","health":"yellow","docs.count":"500000","store.size":"2gb","pri":"3","rep":"0"}
	]`
	m := &mockHTTP{statusCode: http.StatusOK, body: body}
	a := elasticsearch.NewWithClient(m)

	indices, err := a.ListIndices(context.Background(), "")
	if err != nil {
		t.Fatalf("ListIndices() error = %v", err)
	}
	if len(indices) != 2 {
		t.Fatalf("len(indices) = %d, want 2", len(indices))
	}
	if indices[0].Name != "orders-2024" {
		t.Errorf("indices[0].Name = %q, want orders-2024", indices[0].Name)
	}
	if indices[0].Health != "green" {
		t.Errorf("indices[0].Health = %q, want green", indices[0].Health)
	}
	if indices[1].Health != "yellow" {
		t.Errorf("indices[1].Health = %q, want yellow", indices[1].Health)
	}
}

func TestListIndices_WithPattern(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	// Connect first with the test server.
	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if _, err := a.ListIndices(context.Background(), "logs-*"); err != nil {
		t.Fatalf("ListIndices() error = %v", err)
	}

	if !strings.Contains(capturedPath, "logs") {
		t.Errorf("expected path to contain index pattern, got: %s", capturedPath)
	}
}

func TestListIndices_ErrorStatus(t *testing.T) {
	m := &mockHTTP{statusCode: http.StatusForbidden, body: "forbidden"}
	a := elasticsearch.NewWithClient(m)
	_, err := a.ListIndices(context.Background(), "")
	if err == nil {
		t.Error("ListIndices() expected error for 403, got nil")
	}
}

func TestListIndices_InvalidJSON(t *testing.T) {
	m := &mockHTTP{statusCode: http.StatusOK, body: "not json"}
	a := elasticsearch.NewWithClient(m)
	_, err := a.ListIndices(context.Background(), "")
	if err == nil {
		t.Error("ListIndices() expected error for invalid JSON, got nil")
	}
}

func TestConnect_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close before connect to trigger do error

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for closed server, got nil")
	}
}

func TestClusterHealth_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster_name":"test","status":"green","active_shards":5,"unassigned_shards":0,"number_of_nodes":1}`))
	}))

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	srv.Close() // cause subsequent requests to fail

	_, err := a.ClusterHealth(context.Background())
	if err == nil {
		t.Error("ClusterHealth() expected error for closed server, got nil")
	}
}

func TestListIndices_DoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster_name":"test","status":"green","active_shards":5,"unassigned_shards":0,"number_of_nodes":1}`))
	}))

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	srv.Close() // cause subsequent requests to fail

	_, err := a.ListIndices(context.Background(), "")
	if err == nil {
		t.Error("ListIndices() expected error for closed server, got nil")
	}
}

func TestConnect_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster_name":"test","status":"green","active_shards":0,"unassigned_shards":0,"number_of_nodes":1}`))
	}))
	defer srv.Close()

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{"url": srv.URL, "api_key": "myapikey"})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotAuth != "ApiKey myapikey" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "ApiKey myapikey")
	}
}

func TestConnect_WithBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster_name":"test","status":"green","active_shards":0,"unassigned_shards":0,"number_of_nodes":1}`))
	}))
	defer srv.Close()

	a := elasticsearch.New()
	src := store.Component{Config: mustMarshal(t, map[string]any{
		"url":      srv.URL,
		"username": "elastic",
		"password": "changeme",
	})}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotUser != "elastic" {
		t.Errorf("username = %q, want elastic", gotUser)
	}
	if gotPass != "changeme" {
		t.Errorf("password = %q, want changeme", gotPass)
	}
}

func TestListIndices_NumericFields(t *testing.T) {
	// Test toInt64 and toInt with numeric JSON values (float64 from JSON decode)
	body := `[{"index":"test","status":"open","health":"green","docs.count":12345,"store.size":"100mb","pri":3,"rep":1}]`
	m := &mockHTTP{statusCode: http.StatusOK, body: body}
	a := elasticsearch.NewWithClient(m)

	indices, err := a.ListIndices(context.Background(), "")
	if err != nil {
		t.Fatalf("ListIndices() error = %v", err)
	}
	if len(indices) != 1 {
		t.Fatalf("expected 1 index, got %d", len(indices))
	}
	if indices[0].Docs != 12345 {
		t.Errorf("Docs = %d, want 12345", indices[0].Docs)
	}
	if indices[0].Primaries != 3 {
		t.Errorf("Primaries = %d, want 3", indices[0].Primaries)
	}
	if indices[0].Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", indices[0].Replicas)
	}
}

func TestListIndices_NullFields(t *testing.T) {
	// Test toString with null/missing fields
	body := `[{"index":null,"status":null,"health":null,"docs.count":null,"store.size":null,"pri":null,"rep":null}]`
	m := &mockHTTP{statusCode: http.StatusOK, body: body}
	a := elasticsearch.NewWithClient(m)

	indices, err := a.ListIndices(context.Background(), "")
	if err != nil {
		t.Fatalf("ListIndices() error = %v", err)
	}
	if len(indices) != 1 {
		t.Fatalf("expected 1 index, got %d", len(indices))
	}
	// null fields should become zero values
	if indices[0].Name != "" {
		t.Errorf("Name = %q, want empty string for null", indices[0].Name)
	}
	if indices[0].Docs != 0 {
		t.Errorf("Docs = %d, want 0 for null", indices[0].Docs)
	}
}
