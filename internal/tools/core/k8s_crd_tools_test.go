package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/tools/core"
)

// mockCRDK8sClient satisfies all K8s CRD client interfaces (cert-manager, KEDA, OPA, Crossplane).
type mockCRDK8sClient struct {
	resources []map[string]any
	resource  map[string]any
	err       error
}

func (m *mockCRDK8sClient) K8sListResources(_ context.Context, _, _, _ string) ([]map[string]any, error) {
	return m.resources, m.err
}
func (m *mockCRDK8sClient) K8sGetResource(_ context.Context, _, _, _, _ string) (map[string]any, error) {
	return m.resource, m.err
}

// ============================
// cert-manager tool tests
// ============================

func TestCertManagerCertsTool(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		client    *mockCRDK8sClient
		wantCount int
		wantErr   bool
	}{
		{
			name: "lists certificates",
			args: map[string]any{"component_id": "k8s-prod"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "api-tls", "namespace": "production"},
						"spec": map[string]any{
							"dnsNames":   []any{"api.example.com"},
							"secretName": "api-tls-secret",
							"issuerRef":  map[string]any{"name": "letsencrypt", "kind": "ClusterIssuer"},
						},
						"status": map[string]any{
							"notAfter": "2026-06-01T00:00:00Z",
							"conditions": []any{
								map[string]any{"type": "Ready", "status": "True", "reason": "Ready"},
							},
						},
					},
				},
			},
			wantCount: 1,
		},
		{
			name:      "empty list",
			args:      map[string]any{"component_id": "k8s-dev"},
			client:    &mockCRDK8sClient{resources: []map[string]any{}},
			wantCount: 0,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "k8s-prod"},
			client:  &mockCRDK8sClient{err: errors.New("k8s error")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewCertManagerCertsTool(tt.client)
			if tool.Name() != "certmanager_certs" {
				t.Errorf("Name() = %q, want certmanager_certs", tool.Name())
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

func TestCertManagerIssuersTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockCRDK8sClient
		wantErr bool
	}{
		{
			name: "lists issuers",
			args: map[string]any{"component_id": "k8s-prod"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "letsencrypt"},
						"spec":     map[string]any{"acme": map[string]any{"server": "https://acme-v02.api.letsencrypt.org"}},
						"status": map[string]any{
							"conditions": []any{
								map[string]any{"type": "Ready", "status": "True"},
							},
						},
					},
				},
			},
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewCertManagerIssuersTool(tt.client)
			if tool.Name() != "certmanager_issuers" {
				t.Errorf("Name() = %q, want certmanager_issuers", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ============================
// KEDA tool tests
// ============================

func TestKEDAScaledObjectsTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockCRDK8sClient
		wantErr bool
	}{
		{
			name: "lists scaled objects",
			args: map[string]any{"component_id": "k8s-prod"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "worker-scaler", "namespace": "production"},
						"spec": map[string]any{
							"scaleTargetRef":  map[string]any{"name": "worker", "kind": "Deployment"},
							"minReplicaCount": float64(1),
							"maxReplicaCount": float64(10),
							"triggers": []any{
								map[string]any{"type": "kafka"},
							},
						},
						"status": map[string]any{
							"currentReplicas": float64(3),
							"conditions": []any{
								map[string]any{"type": "Ready", "status": "True"},
							},
						},
					},
				},
			},
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewKEDAScaledObjectsTool(tt.client)
			if tool.Name() != "keda_scaledobjects" {
				t.Errorf("Name() = %q, want keda_scaledobjects", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ============================
// OPA tool tests
// ============================

func TestOPAConstraintsTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockCRDK8sClient
		wantErr bool
	}{
		{
			name: "lists all templates",
			args: map[string]any{"component_id": "k8s-prod"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "k8srequiredlabels"},
						"spec": map[string]any{
							"crd": map[string]any{
								"spec": map[string]any{
									"names": map[string]any{"kind": "K8sRequiredLabels"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "lists constraints for specific template",
			args: map[string]any{"component_id": "k8s-prod", "template": "K8sRequiredLabels"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "prod-labels"},
						"status":   map[string]any{"totalViolations": float64(3)},
					},
				},
			},
		},
		{
			name:    "client error listing templates",
			args:    map[string]any{"component_id": "k8s-prod"},
			client:  &mockCRDK8sClient{err: errors.New("k8s unavailable")},
			wantErr: true,
		},
		{
			name:    "client error listing specific template constraints",
			args:    map[string]any{"component_id": "k8s-prod", "template": "K8sRequired"},
			client:  &mockCRDK8sClient{err: errors.New("k8s unavailable")},
			wantErr: true,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewOPAConstraintsTool(tt.client)
			if tool.Name() != "opa_constraints" {
				t.Errorf("Name() = %q, want opa_constraints", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOPAViolationsTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockCRDK8sClient
		wantErr bool
	}{
		{
			name: "returns violations",
			args: map[string]any{
				"component_id": "k8s-prod", "template_kind": "K8sRequiredLabels", "name": "prod-labels",
			},
			client: &mockCRDK8sClient{
				resource: map[string]any{
					"metadata": map[string]any{"name": "prod-labels"},
					"spec":     map[string]any{"enforcementAction": "deny"},
					"status": map[string]any{
						"totalViolations": float64(2),
						"violations": []any{
							map[string]any{"name": "pod-abc", "namespace": "default", "message": "missing required label"},
						},
					},
				},
			},
		},
		{
			name:    "missing component_id",
			args:    map[string]any{"template_kind": "K8sRequired", "name": "test"},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
		{
			name:    "missing template_kind",
			args:    map[string]any{"component_id": "k8s-prod", "name": "test"},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
		{
			name:    "missing name",
			args:    map[string]any{"component_id": "k8s-prod", "template_kind": "K8sRequired"},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
		{
			name: "client error",
			args: map[string]any{
				"component_id": "k8s-prod", "template_kind": "K8sRequiredLabels", "name": "prod-labels",
			},
			client:  &mockCRDK8sClient{err: errors.New("constraint not found")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewOPAViolationsTool(tt.client)
			if tool.Name() != "opa_violations" {
				t.Errorf("Name() = %q, want opa_violations", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ============================
// Crossplane tool tests
// ============================

func TestCrossplaneProvidersTool(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		client    *mockCRDK8sClient
		wantCount int
		wantErr   bool
	}{
		{
			name: "lists providers",
			args: map[string]any{"component_id": "k8s-prod"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "provider-aws"},
						"spec":     map[string]any{"package": "xpkg.upbound.io/upbound/provider-aws:v0.40.0"},
						"status": map[string]any{
							"currentRevision": "provider-aws-abc123",
							"conditions": []any{
								map[string]any{"type": "Healthy", "status": "True"},
								map[string]any{"type": "Installed", "status": "True"},
							},
						},
					},
					{
						"metadata": map[string]any{"name": "provider-gcp"},
						"spec":     map[string]any{"package": "xpkg.upbound.io/upbound/provider-gcp:v0.30.0"},
						"status":   map[string]any{},
					},
				},
			},
			wantCount: 2,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "k8s-prod"},
			client:  &mockCRDK8sClient{err: errors.New("crossplane not installed")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewCrossplaneProvidersTool(tt.client)
			if tool.Name() != "crossplane_providers" {
				t.Errorf("Name() = %q, want crossplane_providers", tool.Name())
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

func TestCrossplaneResourcesTool(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		client  *mockCRDK8sClient
		wantErr bool
	}{
		{
			name: "lists all resources",
			args: map[string]any{"component_id": "k8s-prod"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{
					{
						"metadata": map[string]any{"name": "xpostgresqlinstances.aws.example.org"},
						"spec": map[string]any{
							"group": "aws.example.org",
							"names": map[string]any{"kind": "XPostgreSQLInstance", "plural": "xpostgresqlinstances"},
						},
						"status": map[string]any{
							"conditions": []any{
								map[string]any{"type": "Established", "status": "True", "reason": "WatchSuccess"},
							},
						},
					},
				},
			},
		},
		{
			name: "filters by kind",
			args: map[string]any{"component_id": "k8s-prod", "kind": "Composition"},
			client: &mockCRDK8sClient{
				resources: []map[string]any{},
			},
		},
		{
			name:    "invalid kind",
			args:    map[string]any{"component_id": "k8s-prod", "kind": "Provider"},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			client:  &mockCRDK8sClient{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewCrossplaneResourcesTool(tt.client)
			if tool.Name() != "crossplane_resources" {
				t.Errorf("Name() = %q, want crossplane_resources", tool.Name())
			}
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
