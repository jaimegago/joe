package coreagent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

type fakeAzureAdapter struct {
	vms   []azureadapter.VM
	aks   []azureadapter.AKSCluster
	sql   []azureadapter.SQLDatabase
	vnets []azureadapter.VNet
}

func (f *fakeAzureAdapter) Connect(_ store.Source) error { return nil }

func (f *fakeAzureAdapter) Disconnect() error { return nil }

func (f *fakeAzureAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "connected"}
}

func (f *fakeAzureAdapter) ListVMs(_ context.Context) ([]azureadapter.VM, error) {
	return f.vms, nil
}

func (f *fakeAzureAdapter) GetVM(_ context.Context, _ string) (*azureadapter.VM, error) {
	return nil, nil
}

func (f *fakeAzureAdapter) ListAKSClusters(_ context.Context) ([]azureadapter.AKSCluster, error) {
	return f.aks, nil
}

func (f *fakeAzureAdapter) GetAKSCluster(_ context.Context, _ string) (*azureadapter.AKSCluster, error) {
	return nil, nil
}

func (f *fakeAzureAdapter) ListSQLDatabases(_ context.Context) ([]azureadapter.SQLDatabase, error) {
	return f.sql, nil
}

func (f *fakeAzureAdapter) GetSQLDatabase(_ context.Context, _ string) (*azureadapter.SQLDatabase, error) {
	return nil, nil
}

func (f *fakeAzureAdapter) ListVNets(_ context.Context) ([]azureadapter.VNet, error) {
	return f.vnets, nil
}

func (f *fakeAzureAdapter) GetVNet(_ context.Context, _ string) (*azureadapter.VNet, error) {
	return nil, nil
}

func TestRefreshAzureSourceMapping(t *testing.T) {
	graphStore := setupGraphStore(t)
	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Source{ID: "src-az-1", Type: store.SourceTypeAzure, Name: "test-az"}
	adapter := &fakeAzureAdapter{
		vnets: []azureadapter.VNet{
			{ID: "vnet-1", Name: "vnet-1", Address: "10.1.0.0/16"},
		},
		vms: []azureadapter.VM{
			{ID: "vm-1", Name: "vm-1", Size: "Standard_B2s", State: "running", VNetID: "vnet-1"},
		},
		aks: []azureadapter.AKSCluster{
			{ID: "aks-1", Name: "aks-1", Version: "1.29", Status: "Running", VNetID: "vnet-1"},
		},
		sql: []azureadapter.SQLDatabase{
			{ID: "sql-1", Name: "db-1", ServerName: "server-1", Edition: "Standard", Status: "Online", VNetID: "vnet-1"},
		},
	}

	if err := refresher.refreshAzureSource(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAzureSource error: %v", err)
	}

	nodes, edges, err := LoadGraphStateForSource(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForSource error: %v", err)
	}

	if len(nodes) != 4 {
		t.Fatalf("nodes count = %d, want 4", len(nodes))
	}

	requireEdge(t, edges, "azure/src-az-1/vm/vm-1", "azure/src-az-1/vnet/vnet-1", "in_vnet")
	requireEdge(t, edges, "azure/src-az-1/aks/aks-1", "azure/src-az-1/vnet/vnet-1", "in_vnet")
	requireEdge(t, edges, "azure/src-az-1/sql/sql-1", "azure/src-az-1/vnet/vnet-1", "in_vnet")
}

var _ azureadapter.AzureAdapter = (*fakeAzureAdapter)(nil)
