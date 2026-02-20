package argocd_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/gitops/argocd"
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
