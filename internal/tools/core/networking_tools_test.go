package core_test

import (
	"context"
	"errors"
	"testing"

	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/tools/core"
)

// ---- mock nginx client ----

type mockNginxClient struct {
	ingresses  []nginxadapter.Ingress
	status     *nginxadapter.NginxStatus
	configMaps []nginxadapter.ConfigMapSummary
	err        error
}

func (m *mockNginxClient) NginxIngresses(_ context.Context, _, _ string) ([]nginxadapter.Ingress, error) {
	return m.ingresses, m.err
}
func (m *mockNginxClient) NginxStatus(_ context.Context, _ string) (*nginxadapter.NginxStatus, error) {
	return m.status, m.err
}
func (m *mockNginxClient) NginxConfigMaps(_ context.Context, _, _ string) ([]nginxadapter.ConfigMapSummary, error) {
	return m.configMaps, m.err
}

// ---- mock envoy client ----

type mockEnvoyClient struct {
	clusters   []envoyadapter.ClusterStatus
	configDump map[string]any
	stats      []envoyadapter.Stat
	err        error
}

func (m *mockEnvoyClient) EnvoyClusters(_ context.Context, _ string) ([]envoyadapter.ClusterStatus, error) {
	return m.clusters, m.err
}
func (m *mockEnvoyClient) EnvoyConfigDump(_ context.Context, _, _ string) (map[string]any, error) {
	return m.configDump, m.err
}
func (m *mockEnvoyClient) EnvoyStats(_ context.Context, _, _ string) ([]envoyadapter.Stat, error) {
	return m.stats, m.err
}

// ---- mock istio/cilium K8s client ----

type mockNetK8sClient struct {
	resources []map[string]any
	resource  map[string]any
	err       error
}

func (m *mockNetK8sClient) K8sListResources(_ context.Context, _, _, _ string) ([]map[string]any, error) {
	return m.resources, m.err
}
func (m *mockNetK8sClient) K8sGetResource(_ context.Context, _, _, _, _ string) (map[string]any, error) {
	return m.resource, m.err
}

// ==================
// NGINX tool tests
// ==================

func TestNginxIngressesTool(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		client    *mockNginxClient
		wantCount int
		wantErr   bool
	}{
		{
			name: "lists ingresses",
			args: map[string]any{"source_id": "k8s-prod"},
			client: &mockNginxClient{
				ingresses: []nginxadapter.Ingress{
					{Name: "frontend", Namespace: "production"},
					{Name: "api", Namespace: "production"},
				},
			},
			wantCount: 2,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			client:  &mockNginxClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "k8s-prod"},
			client:  &mockNginxClient{err: errors.New("k8s error")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewNginxIngressesTool(tt.client)
			if tool.Name() != "nginx_ingresses" {
				t.Errorf("Name() = %q", tool.Name())
			}
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				if m["count"].(int) != tt.wantCount {
					t.Errorf("count = %v, want %d", m["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestNginxStatusTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockNginxClient
		wantErr bool
	}{
		{
			name: "returns status",
			args: map[string]any{"source_id": "k8s-prod"},
			client: &mockNginxClient{
				status: &nginxadapter.NginxStatus{ActiveConnections: 100},
			},
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			client:  &mockNginxClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "k8s-prod"},
			client:  &mockNginxClient{err: errors.New("no status URL")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewNginxStatusTool(tt.client)
			if tool.Name() != "nginx_status" {
				t.Errorf("Name() = %q", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNginxConfigTool(t *testing.T) {
	tool := core.NewNginxConfigTool(&mockNginxClient{
		configMaps: []nginxadapter.ConfigMapSummary{
			{Name: "nginx-config", Namespace: "ingress-nginx"},
		},
	})
	if tool.Name() != "nginx_config" {
		t.Errorf("Name() = %q", tool.Name())
	}
	result, err := tool.Execute(context.Background(), map[string]any{"source_id": "k8s-prod"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"].(int) != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

// ==================
// Envoy tool tests
// ==================

func TestEnvoyClustersTool(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		client    *mockEnvoyClient
		wantCount int
		wantErr   bool
	}{
		{
			name: "returns clusters",
			args: map[string]any{"source_id": "envoy-1"},
			client: &mockEnvoyClient{
				clusters: []envoyadapter.ClusterStatus{
					{Name: "backend", HostStatuses: []envoyadapter.HostStatus{{Address: "10.0.0.1:80", HealthStatus: "HEALTHY"}}},
				},
			},
			wantCount: 1,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			client:  &mockEnvoyClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "envoy-1"},
			client:  &mockEnvoyClient{err: errors.New("connection refused")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewEnvoyClustersTool(tt.client)
			if tool.Name() != "envoy_clusters" {
				t.Errorf("Name() = %q", tool.Name())
			}
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				if m["count"].(int) != tt.wantCount {
					t.Errorf("count = %v, want %d", m["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestEnvoyConfigTool(t *testing.T) {
	tool := core.NewEnvoyConfigTool(&mockEnvoyClient{
		configDump: map[string]any{"configs": []any{}},
	})
	if tool.Name() != "envoy_config" {
		t.Errorf("Name() = %q", tool.Name())
	}
	result, err := tool.Execute(context.Background(), map[string]any{"source_id": "envoy-1", "section": "routes"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["section"] != "routes" {
		t.Errorf("section = %v, want routes", m["section"])
	}
}

func TestEnvoyStatsTool(t *testing.T) {
	tool := core.NewEnvoyStatsTool(&mockEnvoyClient{
		stats: []envoyadapter.Stat{
			{Name: "cluster.backend.upstream_cx_active", Value: float64(5)},
		},
	})
	if tool.Name() != "envoy_stats" {
		t.Errorf("Name() = %q", tool.Name())
	}
	result, err := tool.Execute(context.Background(), map[string]any{"source_id": "envoy-1", "filter": "cluster.backend"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"].(int) != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

// ==================
// Istio tool tests
// ==================

func TestIstioConfigTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockNetK8sClient
		wantErr bool
	}{
		{
			name:   "all kinds",
			args:   map[string]any{"source_id": "k8s-prod"},
			client: &mockNetK8sClient{resources: []map[string]any{{"metadata": map[string]any{"name": "my-vs"}}}},
		},
		{
			name:   "specific kind",
			args:   map[string]any{"source_id": "k8s-prod", "kind": "VirtualService"},
			client: &mockNetK8sClient{resources: []map[string]any{}},
		},
		{
			name:    "invalid kind",
			args:    map[string]any{"source_id": "k8s-prod", "kind": "InvalidKind"},
			client:  &mockNetK8sClient{},
			wantErr: true,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			client:  &mockNetK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewIstioConfigTool(tt.client)
			if tool.Name() != "istio_config" {
				t.Errorf("Name() = %q", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIstioResourceTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockNetK8sClient
		wantErr bool
	}{
		{
			name: "found",
			args: map[string]any{
				"source_id": "k8s-prod", "kind": "VirtualService",
				"namespace": "default", "name": "my-vs",
			},
			client: &mockNetK8sClient{resource: map[string]any{"metadata": map[string]any{"name": "my-vs"}}},
		},
		{
			name:    "missing kind",
			args:    map[string]any{"source_id": "k8s-prod", "namespace": "default", "name": "my-vs"},
			client:  &mockNetK8sClient{},
			wantErr: true,
		},
		{
			name: "invalid kind",
			args: map[string]any{
				"source_id": "k8s-prod", "kind": "BadKind",
				"namespace": "default", "name": "my-vs",
			},
			client:  &mockNetK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewIstioResourceTool(tt.client)
			if tool.Name() != "istio_resource" {
				t.Errorf("Name() = %q", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ==================
// Cilium tool tests
// ==================

func TestCiliumPoliciesTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockNetK8sClient
		wantErr bool
	}{
		{
			name: "returns policies",
			args: map[string]any{"source_id": "k8s-prod"},
			client: &mockNetK8sClient{
				resources: []map[string]any{
					{"metadata": map[string]any{"name": "allow-internal"}},
				},
			},
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			client:  &mockNetK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewCiliumPoliciesTool(tt.client)
			if tool.Name() != "cilium_policies" {
				t.Errorf("Name() = %q", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCiliumEndpointsTool(t *testing.T) {
	tool := core.NewCiliumEndpointsTool(&mockNetK8sClient{
		resources: []map[string]any{
			{
				"metadata": map[string]any{"name": "pod-abc123", "namespace": "production"},
				"status":   map[string]any{"id": float64(1234), "state": "ready"},
			},
		},
	})
	if tool.Name() != "cilium_endpoints" {
		t.Errorf("Name() = %q", tool.Name())
	}
	result, err := tool.Execute(context.Background(), map[string]any{"source_id": "k8s-prod"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"].(int) != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}
