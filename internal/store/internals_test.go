package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/safety"
)

// openMigratedStore creates an in-memory SQLite store and runs migrations.
// It registers t.Cleanup to close the store.
func openMigratedStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestPanicStore_ClosedDB verifies that sqlPanicStore methods propagate SQL
// errors when the underlying database is closed.
func TestPanicStore_ClosedDB(t *testing.T) {
	s := openMigratedStore(t)
	ps := s.PanicStore()
	ctx := context.Background()

	// Close the DB to force SQL errors on subsequent calls.
	s.Close()

	if err := ps.SetPanicked(ctx, safety.PanicSourceCLI, "test"); err == nil {
		t.Error("SetPanicked() on closed db: expected error, got nil")
	}
	if err := ps.ClearPanicked(ctx); err == nil {
		t.Error("ClearPanicked() on closed db: expected error, got nil")
	}
	if _, err := ps.IsPanicked(ctx); err == nil {
		t.Error("IsPanicked() on closed db: expected error, got nil")
	}
	if _, err := ps.PanicInfo(ctx); err == nil {
		t.Error("PanicInfo() on closed db: expected error, got nil")
	}
}

// TestPanicStore_NilDB verifies that all sqlPanicStore methods handle a nil db
// gracefully (the early-return branches in panic_store.go).
func TestPanicStore_NilDB(t *testing.T) {
	ps := &sqlPanicStore{db: nil, driver: DriverSQLite}
	ctx := context.Background()

	if err := ps.SetPanicked(ctx, safety.PanicSourceCLI, "test"); err != nil {
		t.Errorf("SetPanicked() with nil db error = %v, want nil", err)
	}

	if err := ps.ClearPanicked(ctx); err != nil {
		t.Errorf("ClearPanicked() with nil db error = %v, want nil", err)
	}

	panicked, err := ps.IsPanicked(ctx)
	if err != nil {
		t.Errorf("IsPanicked() with nil db error = %v, want nil", err)
	}
	if panicked {
		t.Error("IsPanicked() with nil db = true, want false")
	}

	info, err := ps.PanicInfo(ctx)
	if err != nil {
		t.Errorf("PanicInfo() with nil db error = %v, want nil", err)
	}
	if info != nil {
		t.Errorf("PanicInfo() with nil db = %+v, want nil", info)
	}
}

// TestParseTimeOrWarn exercises both the happy path and the error path of the
// package-private parseTimeOrWarn helper.
func TestParseTimeOrWarn(t *testing.T) {
	t.Run("valid RFC3339", func(t *testing.T) {
		got := parseTimeOrWarn("2024-01-15T10:30:00Z", "test_field")
		if got == nil {
			t.Fatal("parseTimeOrWarn() returned nil for valid time")
		}
		if got.Year() != 2024 || got.Month() != 1 || got.Day() != 15 {
			t.Errorf("parsed time = %v, want 2024-01-15", got)
		}
	})

	t.Run("invalid value returns nil", func(t *testing.T) {
		got := parseTimeOrWarn("not-a-time", "test_field")
		if got != nil {
			t.Errorf("parseTimeOrWarn() = %v for invalid input, want nil", got)
		}
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		got := parseTimeOrWarn("", "test_field")
		if got != nil {
			t.Errorf("parseTimeOrWarn() = %v for empty string, want nil", got)
		}
	})
}

// TestEncryptedSourceRepository_EncryptSourceErrorPath triggers the marshal-error
// branch in encryptSource indirectly by wrapping a repo that corrupts data.
// We exercise the encryptSource path with a valid config to confirm 100% hit on
// the happy path, and the nil/empty branch.
func TestEncryptedSourceRepository_EncryptSourcePaths(t *testing.T) {
	key := testKey()
	inner := newMockRepo()
	repo, err := NewEncryptedSourceRepository(inner, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Empty config — encryptSource returns early.
	src := &Source{ID: "empty-cfg", Type: "git", Name: "bare", Config: json.RawMessage("")}
	if err := repo.Create(ctx, src); err != nil {
		t.Fatalf("Create() with empty Config error = %v", err)
	}

	got, err := repo.Get(ctx, "empty-cfg")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}

	// Non-empty config — encryptSource runs encryption.
	src2 := &Source{ID: "real-cfg", Type: "prometheus", Name: "prom", Config: json.RawMessage(`{"token":"abc"}`)}
	if err := repo.Create(ctx, src2); err != nil {
		t.Fatalf("Create() with real Config error = %v", err)
	}

	got2, err := repo.Get(ctx, "real-cfg")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got2.Config) != `{"token":"abc"}` {
		t.Errorf("decrypted config = %s, want {\"token\":\"abc\"}", got2.Config)
	}
}
