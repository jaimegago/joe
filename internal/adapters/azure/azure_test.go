package azure_test

import (
	"context"
	"strings"
	"testing"

	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	"github.com/jaimegago/joe/internal/store"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]any
		expectError bool
	}{
		{
			name: "valid minimal config",
			input: map[string]any{
				"subscription_id": "sub-123",
			},
			expectError: false,
		},
		{
			name:        "missing subscription_id",
			input:       map[string]any{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := azureadapter.ParseConfig(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAdapter_Status(t *testing.T) {
	adapter := azureadapter.New()
	status := adapter.Status()
	if status.Connected {
		t.Errorf("expected disconnected, got connected")
	}
	if status.Message != "Not connected to Azure" {
		t.Errorf("message: got %q, want %q", status.Message, "Not connected to Azure")
	}
}

func TestAdapter_ConnectValidatesConfig(t *testing.T) {
	adapter := azureadapter.New()

	// Invalid JSON
	invalidJSON := store.Component{ID: "az-1", Name: "azure", Type: "azure", Config: []byte(`{"subscription_id":`)}
	if err := adapter.Connect(context.Background(), invalidJSON); err == nil || !strings.Contains(err.Error(), "parse source config JSON") {
		t.Fatalf("expected wrapped JSON parse error, got %v", err)
	}

	// Missing subscription_id
	source := store.Component{ID: "az-1", Name: "azure", Type: "azure", Config: []byte(`{"tenant_id":"t"}`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for missing subscription_id")
	}

	source.Config = []byte(`{"subscription_id":"sub-1"}`)
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status := adapter.Status()
	if !status.Connected || status.Message != "Connected to Azure" {
		t.Fatalf("expected connected status after connect, got %+v", status)
	}
}

func TestAdapter_DisconnectOperations(t *testing.T) {
	adapter := azureadapter.New()
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{name: "ListVMs", fn: func() error { _, err := adapter.ListVMs(ctx); return err }},
		{name: "ListAKSClusters", fn: func() error { _, err := adapter.ListAKSClusters(ctx); return err }},
		{name: "ListSQLDatabases", fn: func() error { _, err := adapter.ListSQLDatabases(ctx); return err }},
		{name: "ListVNets", fn: func() error { _, err := adapter.ListVNets(ctx); return err }},
		{name: "GetVM", fn: func() error { _, err := adapter.GetVM(ctx, "vm-1"); return err }},
		{name: "GetAKSCluster", fn: func() error { _, err := adapter.GetAKSCluster(ctx, "aks-1"); return err }},
		{name: "GetSQLDatabase", fn: func() error { _, err := adapter.GetSQLDatabase(ctx, "sql-1"); return err }},
		{name: "GetVNet", fn: func() error { _, err := adapter.GetVNet(ctx, "vnet-1"); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Fatalf("expected error for %s on disconnected adapter", tt.name)
			}
			if !strings.Contains(err.Error(), "adapter not connected to Azure") {
				t.Fatalf("expected not connected error for %s, got %v", tt.name, err)
			}
		})
	}
}

func TestAdapter_GetMethodsNotFoundWhenConnected(t *testing.T) {
	adapter := azureadapter.New()
	if err := adapter.Connect(context.Background(), store.Component{ID: "az-1", Name: "azure", Type: "azure", Config: []byte(`{"subscription_id":"sub-1"}`)}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name string
		id   string
		fn   func(string) error
		msg  string
	}{
		{name: "GetVM", id: "vm-1", fn: func(id string) error { _, err := adapter.GetVM(ctx, id); return err }, msg: "vm not found: vm-1"},
		{name: "GetAKSCluster", id: "aks-1", fn: func(id string) error { _, err := adapter.GetAKSCluster(ctx, id); return err }, msg: "aks cluster not found: aks-1"},
		{name: "GetSQLDatabase", id: "sql-1", fn: func(id string) error { _, err := adapter.GetSQLDatabase(ctx, id); return err }, msg: "sql database not found: sql-1"},
		{name: "GetVNet", id: "vnet-1", fn: func(id string) error { _, err := adapter.GetVNet(ctx, id); return err }, msg: "vnet not found: vnet-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn(tt.id)
			if err == nil {
				t.Fatalf("expected not found error for %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("expected error to contain %q, got %v", tt.msg, err)
			}
		})
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	adapter := azureadapter.New()
	if err := adapter.Disconnect(); err != nil {
		t.Errorf("unexpected error on disconnect: %v", err)
	}
	status := adapter.Status()
	if status.Connected {
		t.Error("expected disconnected status after disconnect")
	}
}
