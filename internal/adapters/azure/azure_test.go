package azure_test

import (
	"context"
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
	// Missing subscription_id
	source := store.Source{ID: "az-1", Name: "azure", Type: "azure", Config: []byte(`{"tenant_id":"t"}`)}
	if err := adapter.Connect(source); err == nil {
		t.Error("expected error for missing subscription_id")
	}

	source.Config = []byte(`{"subscription_id":"sub-1"}`)
	if err := adapter.Connect(source); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdapter_DisconnectOperations(t *testing.T) {
	adapter := azureadapter.New()
	ctx := context.Background()

	if _, err := adapter.ListVMs(ctx); err == nil {
		t.Error("expected error for ListVMs on disconnected adapter")
	}
	if _, err := adapter.ListAKSClusters(ctx); err == nil {
		t.Error("expected error for ListAKSClusters on disconnected adapter")
	}
	if _, err := adapter.ListSQLDatabases(ctx); err == nil {
		t.Error("expected error for ListSQLDatabases on disconnected adapter")
	}
	if _, err := adapter.ListVNets(ctx); err == nil {
		t.Error("expected error for ListVNets on disconnected adapter")
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
