package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockSecretsLister implements secretsLister.
type mockSecretsLister struct {
	secrets map[string][]corev1.Secret // namespace -> secrets
	err     error
}

func (m *mockSecretsLister) listSecrets(_ context.Context, namespace string, opts metav1.ListOptions) (*corev1.SecretList, error) {
	if m.err != nil {
		return nil, m.err
	}
	var items []corev1.Secret
	if namespace == "" {
		// All namespaces.
		for _, ss := range m.secrets {
			items = append(items, ss...)
		}
	} else {
		items = m.secrets[namespace]
	}

	// Apply basic label filtering (owner=helm selector).
	var filtered []corev1.Secret
	for _, s := range items {
		if matchesSelector(s, opts.LabelSelector) {
			filtered = append(filtered, s)
		}
	}
	return &corev1.SecretList{Items: filtered}, nil
}

// matchesSelector does simple equality label matching for tests.
func matchesSelector(s corev1.Secret, selector string) bool {
	if selector == "" {
		return true
	}
	parts := splitSelector(selector)
	for k, v := range parts {
		if s.Labels[k] != v {
			return false
		}
	}
	return true
}

func splitSelector(selector string) map[string]string {
	result := make(map[string]string)
	for _, part := range splitComma(selector) {
		kv := splitEq(part)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func splitComma(s string) []string {
	var out []string
	for _, p := range splitOn(s, ',') {
		out = append(out, trimSpace(p))
	}
	return out
}

func splitOn(s string, sep rune) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func splitEq(s string) []string {
	return splitOn(s, '=')
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

// encodeHelmRelease creates a base64(gzip(json)) encoded Helm release blob.
func encodeHelmRelease(t *testing.T, rel map[string]any) []byte {
	t.Helper()
	jsonData, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("marshal release: %v", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(jsonData); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	gz.Close()

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return []byte(encoded)
}

func helmSecret(t *testing.T, name, namespace, status string, revision int, relData map[string]any) corev1.Secret {
	t.Helper()
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.release.v1." + name + ".v" + itoa(revision),
			Namespace: namespace,
			Labels: map[string]string{
				"owner":   "helm",
				"name":    name,
				"status":  status,
				"version": itoa(revision),
			},
		},
		Data: map[string][]byte{
			"release": encodeHelmRelease(t, relData),
		},
	}
}

func itoa(i int) string {
	s := ""
	if i == 0 {
		return "0"
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func releaseData(name, namespace, chart, chartVer, status string, revision int) map[string]any {
	return map[string]any{
		"name":    name,
		"version": revision,
		"info": map[string]any{
			"status":        status,
			"last_deployed": "2026-02-20T10:00:00Z",
			"notes":         "Release notes here",
			"description":   "Install complete",
		},
		"chart": map[string]any{
			"metadata": map[string]any{
				"name":       chart,
				"version":    chartVer,
				"appVersion": "1.0.0",
			},
		},
		"config":    map[string]any{"replicas": 3},
		"namespace": namespace,
	}
}

func TestAdapter_Status_NotConnected(t *testing.T) {
	a := New()
	if a.Status().Connected {
		t.Error("expected not connected")
	}
}

func TestAdapter_Releases(t *testing.T) {
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {
				helmSecret(t, "nginx", "production", "deployed", 3,
					releaseData("nginx", "production", "ingress-nginx", "4.8.0", "deployed", 3)),
				helmSecret(t, "nginx", "production", "superseded", 2,
					releaseData("nginx", "production", "ingress-nginx", "4.7.0", "superseded", 2)),
			},
			"staging": {
				helmSecret(t, "cert-manager", "staging", "deployed", 1,
					releaseData("cert-manager", "staging", "cert-manager", "1.13.0", "deployed", 1)),
			},
		},
	}

	cfg := Config{}
	a := NewWithLister(lister, cfg)

	tests := []struct {
		name      string
		namespace string
		wantCount int
		wantErr   bool
	}{
		{name: "all namespaces", namespace: "", wantCount: 2},
		{name: "production only", namespace: "production", wantCount: 1},
		{name: "staging only", namespace: "staging", wantCount: 1},
		{name: "missing namespace", namespace: "missing", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			releases, err := a.Releases(context.Background(), tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("Releases() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(releases) != tt.wantCount {
				t.Errorf("Releases() count = %d, want %d", len(releases), tt.wantCount)
			}
		})
	}
}

func TestAdapter_Releases_LatestRevision(t *testing.T) {
	// Multiple revisions for same release — only latest should appear.
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {
				helmSecret(t, "nginx", "production", "superseded", 1,
					releaseData("nginx", "production", "ingress-nginx", "4.7.0", "superseded", 1)),
				helmSecret(t, "nginx", "production", "superseded", 2,
					releaseData("nginx", "production", "ingress-nginx", "4.8.0", "superseded", 2)),
				helmSecret(t, "nginx", "production", "deployed", 3,
					releaseData("nginx", "production", "ingress-nginx", "4.9.0", "deployed", 3)),
			},
		},
	}

	a := NewWithLister(lister, Config{})
	releases, err := a.Releases(context.Background(), "production")
	if err != nil {
		t.Fatalf("Releases() error = %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release (latest revision), got %d", len(releases))
	}
	if releases[0].Revision != 3 {
		t.Errorf("expected revision 3, got %d", releases[0].Revision)
	}
	if releases[0].ChartVersion != "4.9.0" {
		t.Errorf("expected chart version 4.9.0, got %s", releases[0].ChartVersion)
	}
}

func TestAdapter_GetRelease(t *testing.T) {
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {
				helmSecret(t, "nginx", "production", "deployed", 2,
					releaseData("nginx", "production", "ingress-nginx", "4.8.0", "deployed", 2)),
			},
		},
	}

	a := NewWithLister(lister, Config{})

	tests := []struct {
		name      string
		namespace string
		release   string
		wantErr   bool
	}{
		{name: "found", namespace: "production", release: "nginx"},
		{name: "not found", namespace: "production", release: "missing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail, err := a.GetRelease(context.Background(), tt.namespace, tt.release)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRelease() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if detail.Release.Name != "nginx" {
					t.Errorf("Name = %q, want nginx", detail.Release.Name)
				}
				if detail.Release.ChartVersion != "4.8.0" {
					t.Errorf("ChartVersion = %q, want 4.8.0", detail.Release.ChartVersion)
				}
				if detail.Notes == "" {
					t.Error("expected non-empty notes")
				}
			}
		})
	}
}

func TestAdapter_History(t *testing.T) {
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {
				helmSecret(t, "nginx", "production", "superseded", 1,
					releaseData("nginx", "production", "ingress-nginx", "4.7.0", "superseded", 1)),
				helmSecret(t, "nginx", "production", "superseded", 2,
					releaseData("nginx", "production", "ingress-nginx", "4.8.0", "superseded", 2)),
				helmSecret(t, "nginx", "production", "deployed", 3,
					releaseData("nginx", "production", "ingress-nginx", "4.9.0", "deployed", 3)),
			},
		},
	}

	a := NewWithLister(lister, Config{})

	tests := []struct {
		name      string
		limit     int
		wantCount int
	}{
		{name: "no limit", limit: 0, wantCount: 3},
		{name: "limit 2", limit: 2, wantCount: 2},
		{name: "limit 10 (only 3 entries)", limit: 10, wantCount: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history, err := a.History(context.Background(), "production", "nginx", tt.limit)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			if len(history) != tt.wantCount {
				t.Errorf("History() count = %d, want %d", len(history), tt.wantCount)
			}
			// Should be sorted newest first.
			if len(history) > 0 && history[0].Revision != 3 {
				t.Errorf("first entry revision = %d, want 3", history[0].Revision)
			}
		})
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	a := New()
	ctx := context.Background()

	if _, err := a.Releases(ctx, ""); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Releases(): expected ErrNotConnected, got %v", err)
	}
	if _, err := a.GetRelease(ctx, "ns", "name"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("GetRelease(): expected ErrNotConnected, got %v", err)
	}
	if _, err := a.History(ctx, "ns", "name", 0); !errors.Is(err, ErrNotConnected) {
		t.Errorf("History(): expected ErrNotConnected, got %v", err)
	}
}

func TestAdapter_ListError(t *testing.T) {
	lister := &mockSecretsLister{err: errors.New("connection refused")}
	a := NewWithLister(lister, Config{})

	if _, err := a.Releases(context.Background(), ""); err == nil {
		t.Error("expected error for list failure")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	a := NewWithLister(&mockSecretsLister{}, Config{})
	if err := a.Disconnect(); err != nil {
		t.Errorf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected not connected after Disconnect")
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantErr  bool
		wantPath string
	}{
		{
			name:     "valid with path",
			raw:      `{"kubeconfig_path":"/home/user/.kube/config"}`,
			wantPath: "/home/user/.kube/config",
		},
		{
			name: "empty config (uses default kubeconfig)",
			raw:  `{}`,
		},
		{
			name:    "invalid json",
			raw:     `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.KubeconfigPath != tt.wantPath {
				t.Errorf("KubeconfigPath = %q, want %q", cfg.KubeconfigPath, tt.wantPath)
			}
		})
	}
}
