package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNginxURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/ingresses"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ingresses": []map[string]any{
					{"name": "frontend", "namespace": "default"},
				},
				"component_id": "nginx-1",
			})
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       map[string]any{"active_connections": 42},
				"component_id": "nginx-1",
			})
		case strings.HasSuffix(r.URL.Path, "/config"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config_maps":  []map[string]any{{"name": "nginx-config", "namespace": "ingress-nginx"}},
				"component_id": "nginx-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	ingresses, err := c.NginxIngresses(ctx, "nginx-1", "")
	if err != nil {
		t.Fatalf("NginxIngresses: %v", err)
	}
	if len(ingresses) != 1 {
		t.Errorf("NginxIngresses: got %d, want 1", len(ingresses))
	}

	_, err = c.NginxIngresses(ctx, "nginx-1", "default")
	if err != nil {
		t.Fatalf("NginxIngresses with namespace: %v", err)
	}

	status, err := c.NginxStatus(ctx, "nginx-1")
	if err != nil {
		t.Fatalf("NginxStatus: %v", err)
	}
	if status == nil {
		t.Fatal("NginxStatus returned nil")
	}

	configMaps, err := c.NginxConfigMaps(ctx, "nginx-1", "")
	if err != nil {
		t.Fatalf("NginxConfigMaps: %v", err)
	}
	if len(configMaps) != 1 {
		t.Errorf("NginxConfigMaps: got %d, want 1", len(configMaps))
	}

	_, err = c.NginxConfigMaps(ctx, "nginx-1", "ingress-nginx")
	if err != nil {
		t.Fatalf("NginxConfigMaps with namespace: %v", err)
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/nginx/nginx-1/ingresses")
	assertContains(t, joined, "namespace=default")
	assertContains(t, joined, "/api/v1/nginx/nginx-1/status")
	assertContains(t, joined, "/api/v1/nginx/nginx-1/config")
	assertContains(t, joined, "namespace=ingress-nginx")
}

func TestEnvoyURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/clusters"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"clusters":     []map[string]any{{"name": "payment-svc", "status": "healthy"}},
				"component_id": "envoy-1",
			})
		case strings.HasSuffix(r.URL.Path, "/config"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config":       map[string]any{"@type": "type.googleapis.com/envoy.admin.v3.ConfigDump"},
				"component_id": "envoy-1",
			})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stats":        []map[string]any{{"name": "http.ingress.downstream_cx_active", "value": "5"}},
				"component_id": "envoy-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	clusters, err := c.EnvoyClusters(ctx, "envoy-1")
	if err != nil {
		t.Fatalf("EnvoyClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Errorf("EnvoyClusters: got %d, want 1", len(clusters))
	}

	config, err := c.EnvoyConfigDump(ctx, "envoy-1", "")
	if err != nil {
		t.Fatalf("EnvoyConfigDump: %v", err)
	}
	if config == nil {
		t.Fatal("EnvoyConfigDump returned nil")
	}

	_, err = c.EnvoyConfigDump(ctx, "envoy-1", "listeners")
	if err != nil {
		t.Fatalf("EnvoyConfigDump with section: %v", err)
	}

	stats, err := c.EnvoyStats(ctx, "envoy-1", "")
	if err != nil {
		t.Fatalf("EnvoyStats: %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("EnvoyStats: got %d, want 1", len(stats))
	}

	_, err = c.EnvoyStats(ctx, "envoy-1", "http.ingress")
	if err != nil {
		t.Fatalf("EnvoyStats with filter: %v", err)
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/envoy/envoy-1/clusters")
	assertContains(t, joined, "/api/v1/envoy/envoy-1/config")
	assertContains(t, joined, "section=listeners")
	assertContains(t, joined, "/api/v1/envoy/envoy-1/stats")
	assertContains(t, joined, "filter=http.ingress")
}
