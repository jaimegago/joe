//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

func setupIntegrationServer(t *testing.T) (*api.Server, *http.ServeMux, *store.Store) {
	t.Helper()
	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := testStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	t.Cleanup(func() { testStore.Close() })
	services := core.New(&config.Config{}, testStore, testStore.DB(), testStore.Driver(), adapters.NewRegistry(), nil)
	// Empty config → RBAC disabled → nil engine (the accessor permits every
	// decision), matching this integration store test's pre-rbac-engine-split
	// behaviour.
	server := api.New(services, nil)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return server, mux, testStore
}

func TestIntegration_API_Status(t *testing.T) {
	_, mux, _ := setupIntegrationServer(t)

	// Make request
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}

	if resp["version"] == nil {
		t.Error("expected version field")
	}
}

func TestIntegration_API_NotImplemented(t *testing.T) {
	// All major endpoints are now implemented (phases 1-4 complete):
	// - /api/v1/status - Status endpoint
	// - /api/v1/refresh - Control endpoint (Milestone 2)
	// - /api/v1/graph/* - Graph queries (Milestone 1)
	// - /api/v1/k8s/* - K8s adapter (Milestone 1)
	// - /api/v1/git/* - Git adapter (Milestone 1)
	// - /api/v1/aws/* - AWS adapter (Milestone 1)
	//
	// No unimplemented endpoints to test. This test is now a pass-through.
	t.Skip("All endpoints implemented as of Milestone 4")
}

func TestIntegration_Store_CRUD(t *testing.T) {
	// Setup in-memory store
	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	if err := testStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	ctx := context.Background()

	// Test source CRUD
	t.Run("components", func(t *testing.T) {
		src := &store.Component{
			ID:     "test-k8s",
			Type:   "kubernetes",
			Name:   "Test Cluster",
			Config: json.RawMessage(`{"context":"test"}`),
		}

		// Create
		if err := testStore.Components.Create(ctx, src); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// Read
		got, err := testStore.Components.Get(ctx, "test-k8s")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if got.Name != "Test Cluster" {
			t.Errorf("expected 'Test Cluster', got %q", got.Name)
		}

		// List
		components, err := testStore.Components.List(ctx)
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(components) != 1 {
			t.Errorf("expected 1 source, got %d", len(components))
		}

		// Update
		src.Name = "Updated Cluster"
		if err := testStore.Components.Update(ctx, src); err != nil {
			t.Fatalf("update failed: %v", err)
		}
		got, _ = testStore.Components.Get(ctx, "test-k8s")
		if got.Name != "Updated Cluster" {
			t.Errorf("expected 'Updated Cluster', got %q", got.Name)
		}

		// Delete
		if err := testStore.Components.Delete(ctx, "test-k8s"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		got, _ = testStore.Components.Get(ctx, "test-k8s")
		if got != nil {
			t.Error("expected nil after delete")
		}
	})

	// Test session operations
	t.Run("sessions", func(t *testing.T) {
		session := &store.Session{ID: "test-session"}
		if err := testStore.Sessions.Create(ctx, session); err != nil {
			t.Fatalf("create session failed: %v", err)
		}

		// Add messages
		msg1 := &store.SessionMessage{
			SessionID: "test-session",
			Role:      "user",
			Content:   "Hello",
		}
		if err := testStore.Sessions.AddMessage(ctx, msg1); err != nil {
			t.Fatalf("add message failed: %v", err)
		}

		msg2 := &store.SessionMessage{
			SessionID: "test-session",
			Role:      "assistant",
			Content:   "Hi there!",
		}
		if err := testStore.Sessions.AddMessage(ctx, msg2); err != nil {
			t.Fatalf("add message failed: %v", err)
		}

		// Get messages
		messages, err := testStore.Sessions.GetMessages(ctx, "test-session")
		if err != nil {
			t.Fatalf("get messages failed: %v", err)
		}
		if len(messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(messages))
		}

		// End session
		if err := testStore.Sessions.End(ctx, "test-session", "test completed", nil); err != nil {
			t.Fatalf("end session failed: %v", err)
		}
		session, _ = testStore.Sessions.Get(ctx, "test-session")
		if session.EndedAt == nil {
			t.Error("expected EndedAt to be set")
		}
	})
}

// TestIntegration_Store_Transactions tests transactional behavior
func TestIntegration_Store_Transactions(t *testing.T) {
	testStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	if err := testStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	ctx := context.Background()

	// Create session with messages in a transaction
	tx, err := testStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}

	// Insert a session
	_, err = tx.ExecContext(ctx, "INSERT INTO sessions (id) VALUES (?)", "tx-session")
	if err != nil {
		t.Fatalf("insert session failed: %v", err)
	}

	// Rollback should clean up everything
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	// Session should not exist
	session, _ := testStore.Sessions.Get(ctx, "tx-session")
	if session != nil {
		t.Error("expected session to be rolled back")
	}
}
