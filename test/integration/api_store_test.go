//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
)

func TestIntegration_API_Status(t *testing.T) {
	// Setup API server
	server := api.New()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

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
	server := api.New()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	// Test various unimplemented endpoints
	endpoints := []string{
		"/api/v1/graph/query",
		"/api/v1/sources",
		"/api/v1/clarifications",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", endpoint, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusNotImplemented {
				t.Errorf("expected 501, got %d", w.Code)
			}
		})
	}
}

func TestIntegration_Store_CRUD(t *testing.T) {
	// Setup in-memory store
	testStore, err := store.New(":memory:" + paths.DatabaseFlags)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	if err := testStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	ctx := context.Background()

	// Test source CRUD
	t.Run("sources", func(t *testing.T) {
		src := &store.Source{
			ID:     "test-k8s",
			Type:   "kubernetes",
			Name:   "Test Cluster",
			Config: json.RawMessage(`{"context":"test"}`),
		}

		// Create
		if err := testStore.Sources.Create(ctx, src); err != nil {
			t.Fatalf("create failed: %v", err)
		}

		// Read
		got, err := testStore.Sources.Get(ctx, "test-k8s")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if got.Name != "Test Cluster" {
			t.Errorf("expected 'Test Cluster', got %q", got.Name)
		}

		// List
		sources, err := testStore.Sources.List(ctx)
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(sources) != 1 {
			t.Errorf("expected 1 source, got %d", len(sources))
		}

		// Update
		src.Name = "Updated Cluster"
		if err := testStore.Sources.Update(ctx, src); err != nil {
			t.Fatalf("update failed: %v", err)
		}
		got, _ = testStore.Sources.Get(ctx, "test-k8s")
		if got.Name != "Updated Cluster" {
			t.Errorf("expected 'Updated Cluster', got %q", got.Name)
		}

		// Delete
		if err := testStore.Sources.Delete(ctx, "test-k8s"); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		got, _ = testStore.Sources.Get(ctx, "test-k8s")
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

	// Test clarifications
	t.Run("clarifications", func(t *testing.T) {
		clarification := &store.Clarification{
			ID:       "test-clar",
			Type:     store.ClarificationNewService,
			Context:  json.RawMessage(`{"service":"unknown"}`),
			Question: "What is this service?",
			Options:  []string{"API", "Worker", "Database"},
		}
		if err := testStore.Clarifications.Create(ctx, clarification); err != nil {
			t.Fatalf("create clarification failed: %v", err)
		}

		// List pending
		pending, err := testStore.Clarifications.ListPending(ctx)
		if err != nil {
			t.Fatalf("list pending failed: %v", err)
		}
		if len(pending) != 1 {
			t.Errorf("expected 1 pending, got %d", len(pending))
		}

		// Answer
		if err := testStore.Clarifications.Answer(ctx, "test-clar", "API", "test-user"); err != nil {
			t.Fatalf("answer failed: %v", err)
		}
		c, _ := testStore.Clarifications.Get(ctx, "test-clar")
		if c.Status != store.ClarificationAnswered {
			t.Errorf("expected %q status, got %q", store.ClarificationAnswered, c.Status)
		}
	})
}

// TestIntegration_Store_Transactions tests transactional behavior
func TestIntegration_Store_Transactions(t *testing.T) {
	testStore, err := store.New(":memory:" + paths.DatabaseFlags)
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
