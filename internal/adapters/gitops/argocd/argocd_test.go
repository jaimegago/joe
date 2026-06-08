package argocd_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"net/http/httptest"

	"github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	"github.com/jaimegago/joe/internal/store"
)

// mockHTTP is a fake httpDoer that returns preconfigured responses.
type mockHTTP struct {
	responses map[string]mockResp
}

type mockResp struct {
	status int
	body   string
	err    error
}

func (m *mockHTTP) Do(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	r, ok := m.responses[path]
	if !ok {
		r = mockResp{status: 404, body: `{"message":"not found"}`}
	}
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
	}, nil
}

func testConfig() argocd.Config {
	return argocd.Config{
		URL:   "http://argocd.example.com",
		Token: "secret-token",
	}
}

func appListJSON(apps ...map[string]any) string {
	list := map[string]any{"items": apps}
	b, _ := json.Marshal(list)
	return string(b)
}

func singleAppJSON(name, project, syncStatus, health, revision string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": name, "namespace": "argocd"},
		"spec": map[string]any{
			"project": project,
			"source":  map[string]any{"repoURL": "https://github.com/org/repo", "path": "manifests"},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "production",
			},
		},
		"status": map[string]any{
			"health": map[string]any{"status": health},
			"sync":   map[string]any{"status": syncStatus, "revision": revision},
			"history": []map[string]any{
				{
					"revision":        revision,
					"deployedAt":      "2026-02-20T10:00:00Z",
					"deployStartedAt": "2026-02-20T09:59:00Z",
				},
			},
			"resources": []map[string]any{
				{
					"group":     "apps",
					"kind":      "Deployment",
					"name":      "web",
					"namespace": "production",
					"status":    syncStatus,
					"health":    map[string]any{"status": health},
				},
			},
			"conditions": []map[string]any{},
		},
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
			raw:     `{"url":"http://argo.example.com","token":"tok"}`,
			wantURL: "http://argo.example.com",
		},
		{
			name:    "missing url",
			raw:     `{"token":"tok"}`,
			wantErr: true,
		},
		{
			name:    "missing token",
			raw:     `{"url":"http://argo.example.com"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			raw:     `{invalid}`,
			wantErr: true,
		},
		{
			name:    "empty config",
			raw:     `{}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := argocd.ParseConfig([]byte(tt.raw))
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

func TestAdapter_Status_NotConnected(t *testing.T) {
	a := argocd.New()
	s := a.Status()
	if s.Connected {
		t.Error("expected not connected")
	}
}

func TestAdapter_Apps(t *testing.T) {
	app1 := singleAppJSON("my-app", "default", "Synced", "Healthy", "abc123")
	app2 := singleAppJSON("other-app", "myproject", "OutOfSync", "Degraded", "def456")

	tests := []struct {
		name      string
		project   string
		mock      *mockHTTP
		wantCount int
		wantErr   bool
	}{
		{
			name:    "list all apps",
			project: "",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications": {status: 200, body: appListJSON(app1, app2)},
			}},
			wantCount: 2,
		},
		{
			name:    "list apps by project",
			project: "myproject",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications": {status: 200, body: appListJSON(app2)},
			}},
			wantCount: 1,
		},
		{
			name:    "empty app list",
			project: "",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications": {status: 200, body: `{"items":[]}`},
			}},
			wantCount: 0,
		},
		{
			name:    "API error",
			project: "",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications": {status: 401, body: `{"message":"unauthorized"}`},
			}},
			wantErr: true,
		},
		{
			name:    "network error",
			project: "",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications": {err: errors.New("connection refused")},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := argocd.NewWithClient(tt.mock, testConfig())
			apps, err := a.Apps(context.Background(), tt.project)
			if (err != nil) != tt.wantErr {
				t.Errorf("Apps() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(apps) != tt.wantCount {
				t.Errorf("Apps() count = %d, want %d", len(apps), tt.wantCount)
			}
		})
	}
}

func TestAdapter_Apps_NotConnected(t *testing.T) {
	a := argocd.New()
	_, err := a.Apps(context.Background(), "")
	if !errors.Is(err, argocd.ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}
}

func TestAdapter_GetApp(t *testing.T) {
	appJSON := singleAppJSON("my-app", "default", "Synced", "Healthy", "abc123")
	body, _ := json.Marshal(appJSON)

	tests := []struct {
		name    string
		appName string
		mock    *mockHTTP
		wantErr bool
		check   func(*testing.T, *argocd.AppDetail)
	}{
		{
			name:    "get existing app",
			appName: "my-app",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications/my-app": {status: 200, body: string(body)},
			}},
			check: func(t *testing.T, d *argocd.AppDetail) {
				t.Helper()
				if d.App.Name != "my-app" {
					t.Errorf("Name = %q, want %q", d.App.Name, "my-app")
				}
				if d.App.SyncStatus != "Synced" {
					t.Errorf("SyncStatus = %q, want Synced", d.App.SyncStatus)
				}
				if len(d.Resources) != 1 {
					t.Errorf("Resources count = %d, want 1", len(d.Resources))
				}
			},
		},
		{
			name:    "app not found",
			appName: "missing",
			mock: &mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications/missing": {status: 404, body: `{"message":"application not found"}`},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := argocd.NewWithClient(tt.mock, testConfig())
			detail, err := a.GetApp(context.Background(), tt.appName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetApp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && detail != nil {
				tt.check(t, detail)
			}
		})
	}
}

func TestAdapter_GetDiff(t *testing.T) {
	appJSON := singleAppJSON("my-app", "default", "OutOfSync", "Degraded", "abc123")
	body, _ := json.Marshal(appJSON)

	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/my-app": {status: 200, body: string(body)},
	}}, testConfig())

	diff, err := a.GetDiff(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if diff.SyncStatus != "OutOfSync" {
		t.Errorf("SyncStatus = %q, want OutOfSync", diff.SyncStatus)
	}
	if diff.Revision != "abc123" {
		t.Errorf("Revision = %q, want abc123", diff.Revision)
	}
}

func TestAdapter_GetHistory(t *testing.T) {
	appJSON := singleAppJSON("my-app", "default", "Synced", "Healthy", "abc123")
	body, _ := json.Marshal(appJSON)

	tests := []struct {
		name      string
		limit     int
		wantCount int
	}{
		{name: "no limit", limit: 0, wantCount: 1},
		{name: "limit 1", limit: 1, wantCount: 1},
		{name: "limit 5 (only 1 entry)", limit: 5, wantCount: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
				"/api/v1/applications/my-app": {status: 200, body: string(body)},
			}}, testConfig())

			history, err := a.GetHistory(context.Background(), "my-app", tt.limit)
			if err != nil {
				t.Fatalf("GetHistory() error = %v", err)
			}
			if len(history) != tt.wantCount {
				t.Errorf("GetHistory() count = %d, want %d", len(history), tt.wantCount)
			}
		})
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	a := argocd.NewWithClient(&mockHTTP{}, testConfig())
	if err := a.Disconnect(); err != nil {
		t.Errorf("Disconnect() error = %v", err)
	}
	s := a.Status()
	if s.Connected {
		t.Error("expected not connected after Disconnect")
	}
}

func TestTruncate_LongString(t *testing.T) {
	// Test the truncate function via the doJSON error path which calls truncate.
	// A 400+ response body longer than 200 chars triggers the truncation branch.
	longBody := strings.Repeat("x", 250)
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/long-error": {status: 500, body: longBody},
	}}, testConfig())

	_, err := a.GetApp(context.Background(), "long-error")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	// Verify the error message is truncated (contains "...").
	if !strings.Contains(err.Error(), "...") {
		t.Errorf("expected truncated error message with ..., got: %v", err)
	}
}

func TestGetDiff_Error(t *testing.T) {
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/missing-app": {status: 404, body: `{"message":"not found"}`},
	}}, testConfig())

	_, err := a.GetDiff(context.Background(), "missing-app")
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestGetHistory_Error(t *testing.T) {
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/bad-app": {status: 403, body: `{"message":"forbidden"}`},
	}}, testConfig())

	_, err := a.GetHistory(context.Background(), "bad-app", 0)
	if err == nil {
		t.Error("expected error for 403, got nil")
	}
}

func TestDoJSON_NetworkError(t *testing.T) {
	errHTTP := &mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications": {err: errors.New("connection refused")},
	}}
	a := argocd.NewWithClient(errHTTP, testConfig())
	_, err := a.Apps(context.Background(), "")
	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestConnect_ViaComponent(t *testing.T) {
	// Test Connect via httptest server.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"v2.8.0"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := argocd.New()
	src := store.Component{
		Config: []byte(`{"url":"` + srv.URL + `","token":"tok"}`),
	}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected after Connect()")
	}
}

func TestConnect_BadConfig(t *testing.T) {
	a := argocd.New()
	src := store.Component{Config: []byte(`{}`)} // missing url
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for missing url in config")
	}
}

func TestConnect_PingError(t *testing.T) {
	// Server that returns 500 on /api/version.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := argocd.New()
	src := store.Component{
		Config: []byte(`{"url":"` + srv.URL + `","token":"tok"}`),
	}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error when ping returns 500")
	}
}

func TestGetApp_NotConnected(t *testing.T) {
	a := argocd.New()
	_, err := a.GetApp(context.Background(), "my-app")
	if !errors.Is(err, argocd.ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}
}

func TestGetDiff_NotConnected(t *testing.T) {
	a := argocd.New()
	_, err := a.GetDiff(context.Background(), "my-app")
	if !errors.Is(err, argocd.ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}
}

func TestGetHistory_NotConnected(t *testing.T) {
	a := argocd.New()
	_, err := a.GetHistory(context.Background(), "my-app", 0)
	if !errors.Is(err, argocd.ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}
}

func TestGetDiff_WithOperationState(t *testing.T) {
	// Build an app with operationState set.
	appData := map[string]any{
		"metadata": map[string]any{"name": "my-app", "namespace": "argocd"},
		"spec": map[string]any{
			"project": "default",
			"source":  map[string]any{"repoURL": "https://github.com/org/repo"},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "production",
			},
		},
		"status": map[string]any{
			"health": map[string]any{"status": "Healthy"},
			"sync":   map[string]any{"status": "OutOfSync", "revision": "abc123"},
			"operationState": map[string]any{
				"phase":   "Failed",
				"message": "sync failed: resource conflict",
			},
		},
	}
	body, _ := json.Marshal(appData)
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/my-app": {status: 200, body: string(body)},
	}}, testConfig())

	diff, err := a.GetDiff(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if diff.Message != "sync failed: resource conflict" {
		t.Errorf("diff.Message = %q, want 'sync failed: resource conflict'", diff.Message)
	}
}

func TestGetHistory_WithRepoURL(t *testing.T) {
	// Build app with history entries that have a source.repoURL.
	appData := map[string]any{
		"metadata": map[string]any{"name": "my-app", "namespace": "argocd"},
		"spec": map[string]any{
			"project": "default",
			"source":  map[string]any{"repoURL": "https://github.com/org/repo"},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "production",
			},
		},
		"status": map[string]any{
			"health": map[string]any{"status": "Healthy"},
			"sync":   map[string]any{"status": "Synced", "revision": "abc123"},
			"history": []map[string]any{
				{
					"revision":        "abc123",
					"deployedAt":      "2026-02-20T10:00:00Z",
					"deployStartedAt": "2026-02-20T09:59:00Z",
					"source":          map[string]any{"repoURL": "https://github.com/org/repo"},
				},
			},
		},
	}
	body, _ := json.Marshal(appData)
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/my-app": {status: 200, body: string(body)},
	}}, testConfig())

	history, err := a.GetHistory(context.Background(), "my-app", 0)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(history))
	}
	if history[0].Initiator != "https://github.com/org/repo" {
		t.Errorf("Initiator = %q, want repo URL", history[0].Initiator)
	}
	if history[0].FinishedAt != "2026-02-20T10:00:00Z" {
		t.Errorf("FinishedAt = %q, want 2026-02-20T10:00:00Z", history[0].FinishedAt)
	}
	if history[0].Phase != "Succeeded" {
		t.Errorf("Phase = %q, want Succeeded", history[0].Phase)
	}
}

func TestGetHistory_LimitTruncation(t *testing.T) {
	// Build app with 3 history entries; request limit=2.
	appData := map[string]any{
		"metadata": map[string]any{"name": "my-app", "namespace": "argocd"},
		"spec": map[string]any{
			"project": "default",
			"source":  map[string]any{"repoURL": "https://github.com/org/repo"},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "production",
			},
		},
		"status": map[string]any{
			"health": map[string]any{"status": "Healthy"},
			"sync":   map[string]any{"status": "Synced", "revision": "rev3"},
			"history": []map[string]any{
				{"revision": "rev1", "deployedAt": "2026-01-01T10:00:00Z", "deployStartedAt": "2026-01-01T09:59:00Z"},
				{"revision": "rev2", "deployedAt": "2026-01-02T10:00:00Z", "deployStartedAt": "2026-01-02T09:59:00Z"},
				{"revision": "rev3", "deployedAt": "2026-01-03T10:00:00Z", "deployStartedAt": "2026-01-03T09:59:00Z"},
			},
		},
	}
	body, _ := json.Marshal(appData)
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/my-app": {status: 200, body: string(body)},
	}}, testConfig())

	history, err := a.GetHistory(context.Background(), "my-app", 2)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Errorf("expected 2 entries (limit=2), got %d", len(history))
	}
	// Most recent first.
	if history[0].Revision != "rev3" {
		t.Errorf("first entry Revision = %q, want rev3 (most recent)", history[0].Revision)
	}
}

func TestGetApp_WithNilResourceHealth(t *testing.T) {
	// Resource with nil health field - exercises the nil health check branch.
	appData := map[string]any{
		"metadata": map[string]any{"name": "my-app", "namespace": "argocd"},
		"spec": map[string]any{
			"project": "default",
			"source":  map[string]any{"repoURL": "https://github.com/org/repo"},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": "production",
			},
		},
		"status": map[string]any{
			"health": map[string]any{"status": "Healthy"},
			"sync":   map[string]any{"status": "Synced", "revision": "abc"},
			"resources": []map[string]any{
				{
					"group":     "apps",
					"kind":      "Deployment",
					"name":      "web",
					"namespace": "production",
					"status":    "Synced",
					// no "health" key — nil health pointer branch
				},
			},
			"conditions": []map[string]any{
				{"type": "SyncError", "message": "some condition"},
			},
		},
	}
	body, _ := json.Marshal(appData)
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications/my-app": {status: 200, body: string(body)},
	}}, testConfig())

	detail, err := a.GetApp(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("GetApp() error = %v", err)
	}
	if len(detail.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(detail.Resources))
	}
	if detail.Resources[0].Health != "" {
		t.Errorf("expected empty health for nil health pointer, got %q", detail.Resources[0].Health)
	}
	if len(detail.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(detail.Conditions))
	}
}

func TestDoJSON_DecodeError(t *testing.T) {
	// Return 200 with invalid JSON to trigger the unmarshal error branch.
	a := argocd.NewWithClient(&mockHTTP{responses: map[string]mockResp{
		"/api/v1/applications": {status: 200, body: `{invalid json`},
	}}, testConfig())
	_, err := a.Apps(context.Background(), "")
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}
