package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jaimegago/joe/internal/store"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
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

func TestAdapter_Status_Connected(t *testing.T) {
	a := NewWithLister(&mockSecretsLister{}, Config{})
	s := a.Status()
	if !s.Connected {
		t.Error("expected connected status")
	}
	if s.Message == "" {
		t.Error("expected non-empty status message")
	}
}

func TestDecodeHelmSecret_NoReleaseKey(t *testing.T) {
	// Secret without "release" key — should trigger releaseFromSecret fallback.
	s := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.release.v1.nginx.v1",
			Namespace: "production",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    "nginx",
				"status":  "deployed",
				"version": "1",
			},
		},
		Data: map[string][]byte{}, // no "release" key
	}
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {s},
		},
	}
	a := NewWithLister(lister, Config{})

	// Releases should still work via fallback (label-only release).
	releases, err := a.Releases(context.Background(), "production")
	if err != nil {
		t.Fatalf("Releases() error = %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release (fallback), got %d", len(releases))
	}
	if releases[0].Name != "nginx" {
		t.Errorf("Name = %q, want nginx", releases[0].Name)
	}
}

func TestDecodeHelmSecret_InvalidBase64(t *testing.T) {
	// Data that fails both base64 decode attempts.
	s := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    "badrel",
				"status":  "deployed",
				"version": "1",
			},
		},
		Data: map[string][]byte{
			"release": []byte("!!! not base64 !!!"),
		},
	}
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {s},
		},
	}
	a := NewWithLister(lister, Config{})

	// History calls decodeHelmSecret and skips on error.
	releases, err := a.Releases(context.Background(), "production")
	if err != nil {
		t.Fatalf("Releases() error = %v", err)
	}
	// Fallback to label-only release since decode failed.
	if len(releases) != 1 {
		t.Fatalf("expected 1 fallback release, got %d", len(releases))
	}
}

func TestDecodeHelmSecret_GzipError(t *testing.T) {
	// Valid base64 but not gzip data — triggers gzip reader error.
	notGzip := base64.StdEncoding.EncodeToString([]byte("this is not gzip data"))
	s := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    "badgzip",
				"status":  "deployed",
				"version": "1",
			},
		},
		Data: map[string][]byte{
			"release": []byte(notGzip),
		},
	}
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {s},
		},
	}
	a := NewWithLister(lister, Config{})

	// GetRelease should propagate the decode error.
	_, err := a.GetRelease(context.Background(), "production", "badgzip")
	if err == nil {
		t.Error("expected error for invalid gzip data, got nil")
	}
}

func TestChartName_Empty(t *testing.T) {
	// Chart with empty name — chartName returns "".
	// Exercise via History with a secret whose chart metadata has no name.
	relData := map[string]any{
		"name":    "emptyname",
		"version": 1,
		"info": map[string]any{
			"status":        "deployed",
			"last_deployed": "2026-01-01T00:00:00Z",
			"notes":         "",
			"description":   "",
		},
		"chart": map[string]any{
			"metadata": map[string]any{
				"name":       "",
				"version":    "",
				"appVersion": "",
			},
		},
		"config":    map[string]any{},
		"namespace": "production",
	}
	s := helmSecret(t, "emptyname", "production", "deployed", 1, relData)
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {s},
		},
	}
	a := NewWithLister(lister, Config{})

	history, err := a.History(context.Background(), "production", "emptyname", 0)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Chart != "" {
		t.Errorf("Chart = %q, want empty string for empty chart name", history[0].Chart)
	}
}

func TestAdapter_GetRelease_ListError(t *testing.T) {
	lister := &mockSecretsLister{err: errors.New("k8s api error")}
	a := NewWithLister(lister, Config{})
	_, err := a.GetRelease(context.Background(), "ns", "name")
	if err == nil {
		t.Error("expected error for list failure")
	}
}

func TestAdapter_History_ListError(t *testing.T) {
	lister := &mockSecretsLister{err: errors.New("k8s api error")}
	a := NewWithLister(lister, Config{})
	_, err := a.History(context.Background(), "ns", "name", 0)
	if err == nil {
		t.Error("expected error for list failure")
	}
}

func TestAdapter_History_NotFound(t *testing.T) {
	lister := &mockSecretsLister{secrets: map[string][]corev1.Secret{}}
	a := NewWithLister(lister, Config{})
	_, err := a.History(context.Background(), "production", "missing", 0)
	if err == nil {
		t.Error("expected error for missing release in History")
	}
}

func TestConnect_InvalidConfig(t *testing.T) {
	// Connect with invalid JSON config — should fail at ParseConfig.
	a := New()
	src := store.Source{Config: []byte(`{invalid json`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("Connect() expected error for invalid JSON config, got nil")
	}
}

func TestConnect_BadKubeconfigPath(t *testing.T) {
	// Connect succeeds (lazy init); the bad path surfaces on first operation.
	a := New()
	cfgJSON, _ := json.Marshal(Config{KubeconfigPath: "/nonexistent/path/kubeconfig"})
	src := store.Source{Config: cfgJSON}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	_, err := a.Releases(context.Background(), "")
	if err == nil {
		t.Error("Releases() expected error for non-existent kubeconfig path, got nil")
	}
}

func TestAdapter_Releases_SkipsSecretWithNoName(t *testing.T) {
	// A secret that has owner=helm but no "name" label should be skipped.
	s := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.release.v1.noname.v1",
			Namespace: "production",
			Labels: map[string]string{
				"owner": "helm",
				// "name" intentionally absent
				"status":  "deployed",
				"version": "1",
			},
		},
		Data: map[string][]byte{},
	}
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {s},
		},
	}
	a := NewWithLister(lister, Config{})
	releases, err := a.Releases(context.Background(), "production")
	if err != nil {
		t.Fatalf("Releases() error = %v", err)
	}
	// Secret has no "name" label, so it should be skipped entirely.
	if len(releases) != 0 {
		t.Errorf("expected 0 releases (skipped), got %d", len(releases))
	}
}

func TestAdapter_History_DecodeError_Skipped(t *testing.T) {
	// A secret that fails decode in History should be silently skipped.
	// Mix one valid and one corrupt secret — only the valid one appears.
	validSec := helmSecret(t, "nginx", "production", "deployed", 2,
		releaseData("nginx", "production", "ingress-nginx", "4.8.0", "deployed", 2))
	corruptSec := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.release.v1.nginx.v1",
			Namespace: "production",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    "nginx",
				"status":  "superseded",
				"version": "1",
			},
		},
		Data: map[string][]byte{
			"release": []byte("!!! not base64 !!!"),
		},
	}
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {validSec, corruptSec},
		},
	}
	a := NewWithLister(lister, Config{})
	history, err := a.History(context.Background(), "production", "nginx", 0)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	// Corrupt entry is skipped; only 1 valid entry.
	if len(history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Revision != 2 {
		t.Errorf("expected revision 2, got %d", history[0].Revision)
	}
}

func TestAdapter_GetRelease_MultipleRevisions(t *testing.T) {
	// GetRelease should pick the latest revision among multiple secrets.
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"staging": {
				helmSecret(t, "myapp", "staging", "superseded", 1,
					releaseData("myapp", "staging", "myapp", "1.0.0", "superseded", 1)),
				helmSecret(t, "myapp", "staging", "deployed", 3,
					releaseData("myapp", "staging", "myapp", "3.0.0", "deployed", 3)),
				helmSecret(t, "myapp", "staging", "superseded", 2,
					releaseData("myapp", "staging", "myapp", "2.0.0", "superseded", 2)),
			},
		},
	}
	a := NewWithLister(lister, Config{})
	detail, err := a.GetRelease(context.Background(), "staging", "myapp")
	if err != nil {
		t.Fatalf("GetRelease() error = %v", err)
	}
	if detail.Release.Revision != 3 {
		t.Errorf("expected revision 3 (latest), got %d", detail.Release.Revision)
	}
	if detail.Release.ChartVersion != "3.0.0" {
		t.Errorf("expected chart version 3.0.0, got %s", detail.Release.ChartVersion)
	}
}

func TestDecodeHelmSecret_ValidGzipButInvalidJSON(t *testing.T) {
	// Valid base64(gzip()) but the gzip payload is not valid JSON.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte("not json {{{"))
	gz.Close()
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	s := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "production",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    "badjson",
				"status":  "deployed",
				"version": "1",
			},
		},
		Data: map[string][]byte{
			"release": []byte(encoded),
		},
	}
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"production": {s},
		},
	}
	a := NewWithLister(lister, Config{})
	// GetRelease must propagate the json unmarshal error.
	_, err := a.GetRelease(context.Background(), "production", "badjson")
	if err == nil {
		t.Error("expected error for invalid JSON inside gzip, got nil")
	}
}

func TestRevision_InvalidLabel(t *testing.T) {
	// revision() with a non-numeric label value returns 0.
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				helmVersionLabel: "notanumber",
			},
		},
	}
	if got := revision(s); got != 0 {
		t.Errorf("revision() = %d, want 0 for non-numeric label", got)
	}
}

func TestRevision_MissingLabel(t *testing.T) {
	// revision() with no version label returns 0.
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}
	if got := revision(s); got != 0 {
		t.Errorf("revision() = %d, want 0 for missing label", got)
	}
}

func TestChartName_NonEmpty(t *testing.T) {
	r := &helmReleaseJSON{}
	r.Chart.Metadata.Name = "nginx"
	r.Chart.Metadata.Version = "4.8.0"
	got := chartName(r)
	if got != "nginx-4.8.0" {
		t.Errorf("chartName() = %q, want nginx-4.8.0", got)
	}
}

func TestAdapter_Releases_Sorted(t *testing.T) {
	// Verify releases are returned sorted by name.
	lister := &mockSecretsLister{
		secrets: map[string][]corev1.Secret{
			"ns": {
				helmSecret(t, "zzz", "ns", "deployed", 1, releaseData("zzz", "ns", "zzz", "1.0", "deployed", 1)),
				helmSecret(t, "aaa", "ns", "deployed", 1, releaseData("aaa", "ns", "aaa", "1.0", "deployed", 1)),
				helmSecret(t, "mmm", "ns", "deployed", 1, releaseData("mmm", "ns", "mmm", "1.0", "deployed", 1)),
			},
		},
	}
	a := NewWithLister(lister, Config{})
	releases, err := a.Releases(context.Background(), "ns")
	if err != nil {
		t.Fatalf("Releases() error = %v", err)
	}
	if len(releases) != 3 {
		t.Fatalf("expected 3 releases, got %d", len(releases))
	}
	if releases[0].Name != "aaa" || releases[1].Name != "mmm" || releases[2].Name != "zzz" {
		t.Errorf("releases not sorted: %v", []string{releases[0].Name, releases[1].Name, releases[2].Name})
	}
}

// TestK8sSecretsLister covers the real k8sSecretsLister.listSecrets via a fake clientset.
func TestK8sSecretsLister_ListSecrets(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.release.v1.nginx.v1",
			Namespace: "production",
			Labels: map[string]string{
				"owner":   "helm",
				"name":    "nginx",
				"status":  "deployed",
				"version": "1",
			},
		},
		Data: map[string][]byte{},
	}

	client := fakeclientset.NewSimpleClientset(&secret)
	lister := &k8sSecretsLister{client: client}

	list, err := lister.listSecrets(context.Background(), "production", metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listSecrets() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected 1 secret, got %d", len(list.Items))
	}
	if list.Items[0].Name != "sh.release.v1.nginx.v1" {
		t.Errorf("secret name = %q, want sh.release.v1.nginx.v1", list.Items[0].Name)
	}
}

// TestConnect_WithTempKubeconfig covers the Connect success path using a temporary kubeconfig.
func TestConnect_WithTempKubeconfig(t *testing.T) {
	// Write a minimal kubeconfig pointing to a fake server.
	kubeconfigContent := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:19999
    insecure-skip-tls-verify: true
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`
	f, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("create temp kubeconfig: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(kubeconfigContent); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	f.Close()

	a := New()
	cfgJSON, _ := json.Marshal(Config{KubeconfigPath: f.Name()})
	src := store.Source{Config: cfgJSON}

	// Connect should succeed (kubernetes.NewForConfig only builds the client,
	// it does not dial the server yet).
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() unexpected error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected status after Connect()")
	}
	if a.Status().Message == "" {
		t.Error("expected non-empty status message")
	}
}

// TestBuildRESTConfig_ExplicitPath exercises the explicit kubeconfig path branch.
func TestBuildRESTConfig_ExplicitPath(t *testing.T) {
	kubeconfigContent := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:19999
    insecure-skip-tls-verify: true
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`
	f, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("create temp kubeconfig: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(kubeconfigContent); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	f.Close()

	rc, src, err := buildRESTConfig(Config{KubeconfigPath: f.Name()})
	if err != nil {
		t.Fatalf("buildRESTConfig() error = %v", err)
	}
	if rc == nil {
		t.Error("expected non-nil rest.Config")
	}
	if src != f.Name() {
		t.Errorf("src = %q, want %q", src, f.Name())
	}
}
