package core

import (
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/store"
)

func TestNew(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	db := sqlStore.DB()
	cfg := &config.Config{}
	registry := adapters.NewRegistry()

	svc := New(cfg, sqlStore, db, sqlStore.Driver(), registry, nil)

	if svc == nil {
		t.Fatal("New returned nil")
	}
	if svc.Config != cfg {
		t.Error("Config not set")
	}
	if svc.Store != sqlStore {
		t.Error("Store not set")
	}
	if svc.Graph == nil {
		t.Error("Graph is nil")
	}
	if svc.Adapters != registry {
		t.Error("Adapters not set")
	}
	if svc.Metrics == nil {
		t.Error("Metrics is nil (EnsureMetrics should have created one)")
	}
}

func TestNew_WithMetrics(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	db := sqlStore.DB()
	registry := adapters.NewRegistry()

	svc := New(&config.Config{}, sqlStore, db, sqlStore.Driver(), registry, nil)
	if svc.Metrics == nil {
		t.Error("Metrics should be non-nil when passed as nil (EnsureMetrics)")
	}
}

func TestServices_Close(t *testing.T) {
	svc := &Services{}
	if err := svc.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}
