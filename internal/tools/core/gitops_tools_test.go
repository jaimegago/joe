package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	"github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/adapters/packaging/helm"
	core "github.com/jaimegago/joe/internal/tools/core"
)

// --- Argo CD mock client ---

type mockArgoCDClient struct {
	apps    []argocd.App
	detail  *argocd.AppDetail
	diff    *argocd.Diff
	history []argocd.SyncOperation
	err     error
}

func (m *mockArgoCDClient) ArgoCDApps(_ context.Context, _, _ string) ([]argocd.App, error) {
	return m.apps, m.err
}
func (m *mockArgoCDClient) ArgoCDGetApp(_ context.Context, _, _ string) (*argocd.AppDetail, error) {
	return m.detail, m.err
}
func (m *mockArgoCDClient) ArgoCDGetDiff(_ context.Context, _, _ string) (*argocd.Diff, error) {
	return m.diff, m.err
}
func (m *mockArgoCDClient) ArgoCDGetHistory(_ context.Context, _, _ string, _ int) ([]argocd.SyncOperation, error) {
	return m.history, m.err
}

// --- Flux mock client ---

type mockFluxClient struct {
	list   []map[string]any
	single map[string]any
	err    error
}

func (m *mockFluxClient) K8sListResources(_ context.Context, _, _, _ string) ([]map[string]any, error) {
	return m.list, m.err
}
func (m *mockFluxClient) K8sGetResource(_ context.Context, _, _, _, _ string) (map[string]any, error) {
	return m.single, m.err
}

// --- Terraform mock client ---

type mockTerraformClient struct {
	resources []terraform.Resource
	resource  *terraform.Resource
	outputs   map[string]terraform.Output
	err       error
}

func (m *mockTerraformClient) TerraformResources(_ context.Context, _, _ string) ([]terraform.Resource, error) {
	return m.resources, m.err
}
func (m *mockTerraformClient) TerraformGetResource(_ context.Context, _, _ string) (*terraform.Resource, error) {
	return m.resource, m.err
}
func (m *mockTerraformClient) TerraformOutputs(_ context.Context, _ string) (map[string]terraform.Output, error) {
	return m.outputs, m.err
}

// --- Helm mock client ---

type mockHelmClient struct {
	releases []helm.Release
	detail   *helm.ReleaseDetail
	history  []helm.RevisionEntry
	err      error
}

func (m *mockHelmClient) HelmReleases(_ context.Context, _, _ string) ([]helm.Release, error) {
	return m.releases, m.err
}
func (m *mockHelmClient) HelmGetRelease(_ context.Context, _, _, _ string) (*helm.ReleaseDetail, error) {
	return m.detail, m.err
}
func (m *mockHelmClient) HelmHistory(_ context.Context, _, _, _ string, _ int) ([]helm.RevisionEntry, error) {
	return m.history, m.err
}

// ===================
// Argo CD tool tests
// ===================

func TestArgoCDAppsTool_Name(t *testing.T) {
	tool := core.NewArgoCDAppsTool(&mockArgoCDClient{})
	if tool.Name() != "argocd_apps" {
		t.Errorf("Name() = %q, want argocd_apps", tool.Name())
	}
}

func TestArgoCDAppsTool_Parameters(t *testing.T) {
	tool := core.NewArgoCDAppsTool(&mockArgoCDClient{})
	p := tool.Parameters()
	if _, ok := p.Properties["source_id"]; !ok {
		t.Error("expected source_id in parameters")
	}
	if _, ok := p.Properties["project"]; !ok {
		t.Error("expected project in parameters")
	}
}

func TestArgoCDAppsTool_Execute(t *testing.T) {
	apps := []argocd.App{
		{Name: "my-app", Project: "default", SyncStatus: "Synced", Health: "Healthy"},
		{Name: "other-app", Project: "myproject", SyncStatus: "OutOfSync", Health: "Degraded"},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mock      *mockArgoCDClient
		wantErr   bool
		wantCount int
	}{
		{
			name:      "success",
			args:      map[string]any{"source_id": "argocd-1"},
			mock:      &mockArgoCDClient{apps: apps},
			wantCount: 2,
		},
		{
			name:      "with project filter",
			args:      map[string]any{"source_id": "argocd-1", "project": "myproject"},
			mock:      &mockArgoCDClient{apps: apps[1:]},
			wantCount: 1,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockArgoCDClient{},
			wantErr: true,
		},
		{
			name:    "empty source_id",
			args:    map[string]any{"source_id": ""},
			mock:    &mockArgoCDClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "argocd-1"},
			mock:    &mockArgoCDClient{err: errors.New("unauthorized")},
			wantErr: true,
		},
		{
			name:      "nil apps returns empty list",
			args:      map[string]any{"source_id": "argocd-1"},
			mock:      &mockArgoCDClient{apps: nil},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewArgoCDAppsTool(tt.mock)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				apps := m["apps"].([]argocd.App)
				if len(apps) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(apps), tt.wantCount)
				}
			}
		})
	}
}

func TestArgoCDGetAppTool_Execute(t *testing.T) {
	detail := &argocd.AppDetail{
		App:       argocd.App{Name: "my-app", SyncStatus: "Synced"},
		Resources: []argocd.AppResource{{Kind: "Deployment", Name: "web"}},
	}

	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockArgoCDClient
		wantErr bool
	}{
		{
			name: "success",
			args: map[string]any{"source_id": "argocd-1", "name": "my-app"},
			mock: &mockArgoCDClient{detail: detail},
		},
		{name: "missing source_id", args: map[string]any{"name": "my-app"}, mock: &mockArgoCDClient{}, wantErr: true},
		{name: "missing name", args: map[string]any{"source_id": "argocd-1"}, mock: &mockArgoCDClient{}, wantErr: true},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "argocd-1", "name": "my-app"},
			mock:    &mockArgoCDClient{err: errors.New("not found")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewArgoCDGetAppTool(tt.mock)
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestArgoCDDiffTool_Execute(t *testing.T) {
	diff := &argocd.Diff{Name: "my-app", SyncStatus: "OutOfSync", Revision: "abc123"}

	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockArgoCDClient
		wantErr bool
	}{
		{
			name: "success",
			args: map[string]any{"source_id": "argocd-1", "name": "my-app"},
			mock: &mockArgoCDClient{diff: diff},
		},
		{name: "missing name", args: map[string]any{"source_id": "argocd-1"}, mock: &mockArgoCDClient{}, wantErr: true},
		{
			name:    "error",
			args:    map[string]any{"source_id": "argocd-1", "name": "my-app"},
			mock:    &mockArgoCDClient{err: errors.New("timeout")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewArgoCDDiffTool(tt.mock)
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestArgoCDHistoryTool_Execute(t *testing.T) {
	history := []argocd.SyncOperation{
		{Revision: "abc123", Phase: "Succeeded", StartedAt: "2026-02-20T10:00:00Z"},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mock      *mockArgoCDClient
		wantErr   bool
		wantCount int
	}{
		{
			name:      "success",
			args:      map[string]any{"source_id": "argocd-1", "name": "my-app"},
			mock:      &mockArgoCDClient{history: history},
			wantCount: 1,
		},
		{
			name:      "custom limit",
			args:      map[string]any{"source_id": "argocd-1", "name": "my-app", "limit": float64(5)},
			mock:      &mockArgoCDClient{history: history},
			wantCount: 1,
		},
		{name: "missing name", args: map[string]any{"source_id": "argocd-1"}, mock: &mockArgoCDClient{}, wantErr: true},
		{
			name:      "nil history",
			args:      map[string]any{"source_id": "argocd-1", "name": "my-app"},
			mock:      &mockArgoCDClient{history: nil},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewArgoCDHistoryTool(tt.mock)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				h := m["history"].([]argocd.SyncOperation)
				if len(h) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(h), tt.wantCount)
				}
			}
		})
	}
}

// ===================
// Flux tool tests
// ===================

func TestFluxStatusTool_Name(t *testing.T) {
	tool := core.NewFluxStatusTool(&mockFluxClient{})
	if tool.Name() != "flux_status" {
		t.Errorf("Name() = %q, want flux_status", tool.Name())
	}
}

func TestFluxStatusTool_Execute(t *testing.T) {
	resources := []map[string]any{
		{
			"metadata": map[string]any{"name": "my-kustomization", "namespace": "flux-system"},
			"status": map[string]any{
				"lastAppliedRevision": "main@sha1:abc123",
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "True", "reason": "ReconciliationSucceeded"},
				},
			},
		},
	}

	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockFluxClient
		wantErr bool
	}{
		{
			name: "success",
			args: map[string]any{"source_id": "k8s-1"},
			mock: &mockFluxClient{list: resources},
		},
		{
			name: "with namespace",
			args: map[string]any{"source_id": "k8s-1", "namespace": "flux-system"},
			mock: &mockFluxClient{list: resources},
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockFluxClient{},
			wantErr: true,
		},
		{
			// CRD not installed — error is silently skipped per flux tool logic
			name: "list error is skipped",
			args: map[string]any{"source_id": "k8s-1"},
			mock: &mockFluxClient{err: errors.New("no resource type")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewFluxStatusTool(tt.mock)
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFluxResourceTool_Execute(t *testing.T) {
	resource := map[string]any{
		"metadata": map[string]any{"name": "my-kustomization", "namespace": "flux-system"},
		"status":   map[string]any{"lastAppliedRevision": "abc123"},
	}

	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockFluxClient
		wantErr bool
	}{
		{
			name: "success",
			args: map[string]any{
				"source_id": "k8s-1", "kind": "Kustomization",
				"namespace": "flux-system", "name": "my-kustomization",
			},
			mock: &mockFluxClient{single: resource},
		},
		{
			name: "unsupported kind",
			args: map[string]any{
				"source_id": "k8s-1", "kind": "InvalidKind",
				"namespace": "flux-system", "name": "my-thing",
			},
			mock:    &mockFluxClient{},
			wantErr: true,
		},
		{
			name: "missing kind",
			args: map[string]any{
				"source_id": "k8s-1", "namespace": "flux-system", "name": "my-kustomization",
			},
			mock:    &mockFluxClient{},
			wantErr: true,
		},
		{
			name: "client error",
			args: map[string]any{
				"source_id": "k8s-1", "kind": "Kustomization",
				"namespace": "flux-system", "name": "my-kustomization",
			},
			mock:    &mockFluxClient{err: errors.New("not found")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewFluxResourceTool(tt.mock)
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ===================
// Terraform tool tests
// ===================

func TestTerraformStateTool_Name(t *testing.T) {
	tool := core.NewTerraformStateTool(&mockTerraformClient{})
	if tool.Name() != "terraform_state" {
		t.Errorf("Name() = %q, want terraform_state", tool.Name())
	}
}

func TestTerraformStateTool_Execute(t *testing.T) {
	resources := []terraform.Resource{
		{Address: "aws_instance.web", Type: "aws_instance", Name: "web"},
		{Address: "aws_db_instance.main", Type: "aws_db_instance", Name: "main"},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mock      *mockTerraformClient
		wantErr   bool
		wantCount int
	}{
		{
			name:      "all resources",
			args:      map[string]any{"source_id": "tf-1"},
			mock:      &mockTerraformClient{resources: resources},
			wantCount: 2,
		},
		{
			name:      "with type filter",
			args:      map[string]any{"source_id": "tf-1", "resource_type": "aws_instance"},
			mock:      &mockTerraformClient{resources: resources[:1]},
			wantCount: 1,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockTerraformClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "tf-1"},
			mock:    &mockTerraformClient{err: errors.New("file not found")},
			wantErr: true,
		},
		{
			name:      "nil resources",
			args:      map[string]any{"source_id": "tf-1"},
			mock:      &mockTerraformClient{resources: nil},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewTerraformStateTool(tt.mock)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				rs := m["resources"].([]terraform.Resource)
				if len(rs) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(rs), tt.wantCount)
				}
			}
		})
	}
}

func TestTerraformResourceTool_Execute(t *testing.T) {
	resource := &terraform.Resource{
		Address: "aws_instance.web", Type: "aws_instance",
		Instances: []terraform.ResourceInstance{{Attributes: map[string]any{"id": "i-123"}}},
	}

	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockTerraformClient
		wantErr bool
	}{
		{
			name: "success",
			args: map[string]any{"source_id": "tf-1", "address": "aws_instance.web"},
			mock: &mockTerraformClient{resource: resource},
		},
		{name: "missing address", args: map[string]any{"source_id": "tf-1"}, mock: &mockTerraformClient{}, wantErr: true},
		{
			name:    "not found",
			args:    map[string]any{"source_id": "tf-1", "address": "aws_instance.missing"},
			mock:    &mockTerraformClient{err: errors.New("not found")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewTerraformResourceTool(tt.mock)
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTerraformOutputsTool_Execute(t *testing.T) {
	outputs := map[string]terraform.Output{
		"public_ip":   {Value: "1.2.3.4", Type: "string"},
		"db_password": {Value: "[redacted]", Type: "string", Sensitive: true},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mock      *mockTerraformClient
		wantErr   bool
		wantCount int
	}{
		{
			name:      "success",
			args:      map[string]any{"source_id": "tf-1"},
			mock:      &mockTerraformClient{outputs: outputs},
			wantCount: 2,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockTerraformClient{},
			wantErr: true,
		},
		{
			name:      "nil outputs",
			args:      map[string]any{"source_id": "tf-1"},
			mock:      &mockTerraformClient{outputs: nil},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewTerraformOutputsTool(tt.mock)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				outs := m["outputs"].(map[string]terraform.Output)
				if len(outs) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(outs), tt.wantCount)
				}
			}
		})
	}
}

// ===================
// Helm tool tests
// ===================

func TestHelmReleasesTool_Name(t *testing.T) {
	tool := core.NewHelmReleasesTool(&mockHelmClient{})
	if tool.Name() != "helm_releases" {
		t.Errorf("Name() = %q, want helm_releases", tool.Name())
	}
}

func TestHelmReleasesTool_Execute(t *testing.T) {
	releases := []helm.Release{
		{Name: "nginx", Namespace: "production", Chart: "ingress-nginx", Status: "deployed", Revision: 3},
		{Name: "cert-manager", Namespace: "cert-manager", Chart: "cert-manager", Status: "deployed", Revision: 1},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mock      *mockHelmClient
		wantErr   bool
		wantCount int
	}{
		{
			name:      "all namespaces",
			args:      map[string]any{"source_id": "helm-1"},
			mock:      &mockHelmClient{releases: releases},
			wantCount: 2,
		},
		{
			name:      "with namespace",
			args:      map[string]any{"source_id": "helm-1", "namespace": "production"},
			mock:      &mockHelmClient{releases: releases[:1]},
			wantCount: 1,
		},
		{
			name:    "missing source_id",
			args:    map[string]any{},
			mock:    &mockHelmClient{},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"source_id": "helm-1"},
			mock:    &mockHelmClient{err: errors.New("connection refused")},
			wantErr: true,
		},
		{
			name:      "nil releases",
			args:      map[string]any{"source_id": "helm-1"},
			mock:      &mockHelmClient{releases: nil},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewHelmReleasesTool(tt.mock)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				rs := m["releases"].([]helm.Release)
				if len(rs) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(rs), tt.wantCount)
				}
			}
		})
	}
}

func TestHelmGetReleaseTool_Execute(t *testing.T) {
	detail := &helm.ReleaseDetail{
		Release: helm.Release{Name: "nginx", Namespace: "production"},
		Values:  map[string]any{"replicas": 3},
		Notes:   "Release notes",
	}

	tests := []struct {
		name    string
		args    map[string]any
		mock    *mockHelmClient
		wantErr bool
	}{
		{
			name: "success",
			args: map[string]any{"source_id": "helm-1", "namespace": "production", "name": "nginx"},
			mock: &mockHelmClient{detail: detail},
		},
		{name: "missing namespace", args: map[string]any{"source_id": "helm-1", "name": "nginx"}, mock: &mockHelmClient{}, wantErr: true},
		{name: "missing name", args: map[string]any{"source_id": "helm-1", "namespace": "production"}, mock: &mockHelmClient{}, wantErr: true},
		{
			name:    "not found",
			args:    map[string]any{"source_id": "helm-1", "namespace": "production", "name": "missing"},
			mock:    &mockHelmClient{err: errors.New("not found")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewHelmGetReleaseTool(tt.mock)
			_, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHelmHistoryTool_Execute(t *testing.T) {
	history := []helm.RevisionEntry{
		{Revision: 3, Status: "deployed", Chart: "ingress-nginx-4.9.0"},
		{Revision: 2, Status: "superseded", Chart: "ingress-nginx-4.8.0"},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mock      *mockHelmClient
		wantErr   bool
		wantCount int
	}{
		{
			name:      "success",
			args:      map[string]any{"source_id": "helm-1", "namespace": "production", "name": "nginx"},
			mock:      &mockHelmClient{history: history},
			wantCount: 2,
		},
		{
			name:      "with limit",
			args:      map[string]any{"source_id": "helm-1", "namespace": "production", "name": "nginx", "limit": float64(1)},
			mock:      &mockHelmClient{history: history[:1]},
			wantCount: 1,
		},
		{
			name:    "missing name",
			args:    map[string]any{"source_id": "helm-1", "namespace": "production"},
			mock:    &mockHelmClient{},
			wantErr: true,
		},
		{
			name:      "nil history",
			args:      map[string]any{"source_id": "helm-1", "namespace": "production", "name": "nginx"},
			mock:      &mockHelmClient{history: nil},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := core.NewHelmHistoryTool(tt.mock)
			result, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				m := result.(map[string]any)
				h := m["history"].([]helm.RevisionEntry)
				if len(h) != tt.wantCount {
					t.Errorf("count = %d, want %d", len(h), tt.wantCount)
				}
			}
		})
	}
}
