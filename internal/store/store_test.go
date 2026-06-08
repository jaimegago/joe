package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/store"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewAndMigrate(t *testing.T) {
	s := setupTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if s.DB() == nil {
		t.Fatal("expected non-nil db")
	}
}

func TestComponentRepository(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	t.Run("create and get", func(t *testing.T) {
		src := &store.Component{
			ID:     "k8s-prod",
			Type:   "kubernetes",
			Name:   "Production Cluster",
			Config: json.RawMessage(`{"context": "prod"}`),
		}
		if err := s.Components.Create(ctx, src); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := s.Components.Get(ctx, "k8s-prod")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got == nil {
			t.Fatal("Get() returned nil")
		}
		if got.ID != "k8s-prod" {
			t.Errorf("ID = %q, want %q", got.ID, "k8s-prod")
		}
		if got.Name != "Production Cluster" {
			t.Errorf("Name = %q, want %q", got.Name, "Production Cluster")
		}
		if got.Status != "active" {
			t.Errorf("Status = %q, want %q", got.Status, "active")
		}
	})

	t.Run("get nonexistent returns nil", func(t *testing.T) {
		got, err := s.Components.Get(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got != nil {
			t.Errorf("Get() = %v, want nil", got)
		}
	})

	t.Run("list", func(t *testing.T) {
		components, err := s.Components.List(ctx)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(components) != 1 {
			t.Errorf("List() returned %d components, want 1", len(components))
		}
	})

	t.Run("list by type", func(t *testing.T) {
		components, err := s.Components.ListByType(ctx, "kubernetes")
		if err != nil {
			t.Fatalf("ListByType() error = %v", err)
		}
		if len(components) != 1 {
			t.Errorf("ListByType() returned %d components, want 1", len(components))
		}

		components, err = s.Components.ListByType(ctx, "git")
		if err != nil {
			t.Fatalf("ListByType() error = %v", err)
		}
		if len(components) != 0 {
			t.Errorf("ListByType(git) returned %d components, want 0", len(components))
		}
	})

	t.Run("update", func(t *testing.T) {
		src := &store.Component{
			ID:     "k8s-prod",
			Type:   "kubernetes",
			Name:   "Prod Cluster (updated)",
			Config: json.RawMessage(`{"context": "prod-v2"}`),
			Status: "active",
		}
		if err := s.Components.Update(ctx, src); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		got, _ := s.Components.Get(ctx, "k8s-prod")
		if got.Name != "Prod Cluster (updated)" {
			t.Errorf("Name = %q, want %q", got.Name, "Prod Cluster (updated)")
		}
	})

	t.Run("update sync status with error", func(t *testing.T) {
		if err := s.Components.UpdateSyncStatus(ctx, "k8s-prod", time.Now(), "connection refused"); err != nil {
			t.Fatalf("UpdateSyncStatus() error = %v", err)
		}

		got, _ := s.Components.Get(ctx, "k8s-prod")
		if got.Status != "error" {
			t.Errorf("Status = %q, want %q", got.Status, "error")
		}
		if got.LastError != "connection refused" {
			t.Errorf("LastError = %q, want %q", got.LastError, "connection refused")
		}
	})

	t.Run("update sync status success", func(t *testing.T) {
		if err := s.Components.UpdateSyncStatus(ctx, "k8s-prod", time.Now(), ""); err != nil {
			t.Fatalf("UpdateSyncStatus() error = %v", err)
		}

		got, _ := s.Components.Get(ctx, "k8s-prod")
		if got.Status != "active" {
			t.Errorf("Status = %q, want %q", got.Status, "active")
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := s.Components.Delete(ctx, "k8s-prod"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		got, _ := s.Components.Get(ctx, "k8s-prod")
		if got != nil {
			t.Error("expected nil after delete")
		}
	})
}

func TestClarificationRepository(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	t.Run("create and list pending", func(t *testing.T) {
		c := &store.Clarification{
			ID:       "clar-1",
			Type:     store.ClarificationNewService,
			Context:  json.RawMessage(`{"deployment": "mystery-svc"}`),
			Question: "What is mystery-svc?",
			Options:  []string{"API Gateway", "Auth Service", "Unknown"},
		}
		if err := s.Clarifications.Create(ctx, c); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		pending, err := s.Clarifications.ListPending(ctx)
		if err != nil {
			t.Fatalf("ListPending() error = %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("ListPending() returned %d, want 1", len(pending))
		}
		if pending[0].Question != "What is mystery-svc?" {
			t.Errorf("Question = %q, want %q", pending[0].Question, "What is mystery-svc?")
		}
		if !reflect.DeepEqual(pending[0].Options, []string{"API Gateway", "Auth Service", "Unknown"}) {
			t.Errorf("Options = %v, want [API Gateway Auth Service Unknown]", pending[0].Options)
		}
	})

	t.Run("get", func(t *testing.T) {
		c, err := s.Clarifications.Get(ctx, "clar-1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if c == nil {
			t.Fatal("Get() returned nil")
		}
		if c.Status != store.ClarificationPending {
			t.Errorf("Status = %q, want %q", c.Status, store.ClarificationPending)
		}
	})

	t.Run("get nonexistent returns nil", func(t *testing.T) {
		c, err := s.Clarifications.Get(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if c != nil {
			t.Errorf("Get() = %v, want nil", c)
		}
	})

	t.Run("answer clarification", func(t *testing.T) {
		if err := s.Clarifications.Answer(ctx, "clar-1", "Auth Service", "user"); err != nil {
			t.Fatalf("Answer() error = %v", err)
		}

		c, _ := s.Clarifications.Get(ctx, "clar-1")
		if c.Status != store.ClarificationAnswered {
			t.Errorf("Status = %q, want %q", c.Status, store.ClarificationAnswered)
		}
		if c.Answer != "Auth Service" {
			t.Errorf("Answer = %q, want %q", c.Answer, "Auth Service")
		}
		if c.AnsweredBy != "user" {
			t.Errorf("AnsweredBy = %q, want %q", c.AnsweredBy, "user")
		}

		pending, _ := s.Clarifications.ListPending(ctx)
		if len(pending) != 0 {
			t.Errorf("ListPending() returned %d, want 0", len(pending))
		}
	})

	t.Run("dismiss clarification", func(t *testing.T) {
		c2 := &store.Clarification{
			ID:       "clar-2",
			Type:     store.ClarificationEdgeConfirm,
			Context:  json.RawMessage(`{}`),
			Question: "Does svc-a depend on svc-b?",
		}
		s.Clarifications.Create(ctx, c2)

		if err := s.Clarifications.Dismiss(ctx, "clar-2"); err != nil {
			t.Fatalf("Dismiss() error = %v", err)
		}

		c, _ := s.Clarifications.Get(ctx, "clar-2")
		if c.Status != store.ClarificationDismissed {
			t.Errorf("Status = %q, want %q", c.Status, store.ClarificationDismissed)
		}
	})

	t.Run("mark notified", func(t *testing.T) {
		c3 := &store.Clarification{
			ID:       "clar-3",
			Type:     store.ClarificationNewComponent,
			Context:  json.RawMessage(`{}`),
			Question: "New source detected?",
		}
		s.Clarifications.Create(ctx, c3)

		if err := s.Clarifications.MarkNotified(ctx, "clar-3"); err != nil {
			t.Fatalf("MarkNotified() error = %v", err)
		}

		c, _ := s.Clarifications.Get(ctx, "clar-3")
		if c.NotifiedAt == nil {
			t.Error("NotifiedAt should not be nil")
		}
	})
}

func TestSessionRepository(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	t.Run("create session and add messages", func(t *testing.T) {
		session := &store.Session{ID: "sess-1"}
		if err := s.Sessions.Create(ctx, session); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if err := s.Sessions.AddMessage(ctx, &store.SessionMessage{
			SessionID: "sess-1",
			Role:      "user",
			Content:   "Why is payment slow?",
		}); err != nil {
			t.Fatalf("AddMessage() error = %v", err)
		}

		if err := s.Sessions.AddMessage(ctx, &store.SessionMessage{
			SessionID: "sess-1",
			Role:      "assistant",
			Content:   "Let me check...",
		}); err != nil {
			t.Fatalf("AddMessage() error = %v", err)
		}

		messages, err := s.Sessions.GetMessages(ctx, "sess-1")
		if err != nil {
			t.Fatalf("GetMessages() error = %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("GetMessages() returned %d, want 2", len(messages))
		}
		if messages[0].Role != "user" {
			t.Errorf("messages[0].Role = %q, want %q", messages[0].Role, "user")
		}
		if messages[1].Content != "Let me check..." {
			t.Errorf("messages[1].Content = %q, want %q", messages[1].Content, "Let me check...")
		}
	})

	t.Run("get session", func(t *testing.T) {
		session, err := s.Sessions.Get(ctx, "sess-1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if session == nil {
			t.Fatal("Get() returned nil")
		}
		if session.EndedAt != nil {
			t.Error("EndedAt should be nil for active session")
		}
	})

	t.Run("get nonexistent session returns nil", func(t *testing.T) {
		session, err := s.Sessions.Get(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if session != nil {
			t.Errorf("Get() = %v, want nil", session)
		}
	})

	t.Run("end session with summary", func(t *testing.T) {
		if err := s.Sessions.End(ctx, "sess-1", "Investigated payment latency", nil); err != nil {
			t.Fatalf("End() error = %v", err)
		}

		session, _ := s.Sessions.Get(ctx, "sess-1")
		if session.EndedAt == nil {
			t.Error("EndedAt should not be nil after ending")
		}
		if session.Summary != "Investigated payment latency" {
			t.Errorf("Summary = %q, want %q", session.Summary, "Investigated payment latency")
		}
	})

	t.Run("list recent", func(t *testing.T) {
		s.Sessions.Create(ctx, &store.Session{ID: "sess-2"})

		sessions, err := s.Sessions.ListRecent(ctx, 10)
		if err != nil {
			t.Fatalf("ListRecent() error = %v", err)
		}
		if len(sessions) != 2 {
			t.Errorf("ListRecent() returned %d, want 2", len(sessions))
		}
	})

	t.Run("add message with tool info", func(t *testing.T) {
		msg := &store.SessionMessage{
			SessionID: "sess-2",
			Role:      "tool_call",
			Content:   "Calling graph_query",
			ToolName:  "graph_query",
			ToolArgs:  json.RawMessage(`{"query": "payments"}`),
		}
		if err := s.Sessions.AddMessage(ctx, msg); err != nil {
			t.Fatalf("AddMessage() error = %v", err)
		}
		if msg.ID == 0 {
			t.Error("expected non-zero ID after insert")
		}

		messages, _ := s.Sessions.GetMessages(ctx, "sess-2")
		if len(messages) != 1 {
			t.Fatalf("GetMessages() returned %d, want 1", len(messages))
		}
		if messages[0].ToolName != "graph_query" {
			t.Errorf("ToolName = %q, want %q", messages[0].ToolName, "graph_query")
		}
	})
}

func TestCacheRepository(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	t.Run("set and get", func(t *testing.T) {
		cache := &store.JoeFileCache{
			FilePath:    "/repo/.joe/context.md",
			ContentHash: "abc123",
			ParsedData:  json.RawMessage(`{"service": "payments"}`),
		}
		if err := s.Cache.Set(ctx, cache); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		got, err := s.Cache.Get(ctx, "/repo/.joe/context.md")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got == nil {
			t.Fatal("Get() returned nil")
		}
		if got.ContentHash != "abc123" {
			t.Errorf("ContentHash = %q, want %q", got.ContentHash, "abc123")
		}
	})

	t.Run("get missing returns nil", func(t *testing.T) {
		got, err := s.Cache.Get(ctx, "/nonexistent")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got != nil {
			t.Errorf("Get() = %v, want nil", got)
		}
	})

	t.Run("upsert overwrites", func(t *testing.T) {
		cache := &store.JoeFileCache{
			FilePath:    "/repo/.joe/context.md",
			ContentHash: "def456",
			ParsedData:  json.RawMessage(`{"service": "payments-v2"}`),
		}
		if err := s.Cache.Set(ctx, cache); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		got, _ := s.Cache.Get(ctx, "/repo/.joe/context.md")
		if got.ContentHash != "def456" {
			t.Errorf("ContentHash = %q, want %q", got.ContentHash, "def456")
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := s.Cache.Delete(ctx, "/repo/.joe/context.md"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		got, _ := s.Cache.Get(ctx, "/repo/.joe/context.md")
		if got != nil {
			t.Error("expected nil after delete")
		}
	})

	t.Run("delete all", func(t *testing.T) {
		s.Cache.Set(ctx, &store.JoeFileCache{
			FilePath: "/a", ContentHash: "h1", ParsedData: json.RawMessage(`{}`),
		})
		s.Cache.Set(ctx, &store.JoeFileCache{
			FilePath: "/b", ContentHash: "h2", ParsedData: json.RawMessage(`{}`),
		})

		if err := s.Cache.DeleteAll(ctx); err != nil {
			t.Fatalf("DeleteAll() error = %v", err)
		}

		a, _ := s.Cache.Get(ctx, "/a")
		b, _ := s.Cache.Get(ctx, "/b")
		if a != nil || b != nil {
			t.Error("expected nil after DeleteAll")
		}
	})
}

func TestFactRepository(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	t.Run("create and get by subject", func(t *testing.T) {
		fact := &store.OnboardingFact{
			FactType: "service_purpose",
			Subject:  "payments",
			Content:  "Handles credit card processing",
			Source:   "onboarding",
		}
		if err := s.Facts.Create(ctx, fact); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if fact.ID == 0 {
			t.Error("expected non-zero ID after insert")
		}

		facts, err := s.Facts.GetBySubject(ctx, "payments")
		if err != nil {
			t.Fatalf("GetBySubject() error = %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("GetBySubject() returned %d, want 1", len(facts))
		}
		if facts[0].Content != "Handles credit card processing" {
			t.Errorf("Content = %q, want %q", facts[0].Content, "Handles credit card processing")
		}
	})

	t.Run("get by type", func(t *testing.T) {
		s.Facts.Create(ctx, &store.OnboardingFact{
			FactType:    "team_ownership",
			Subject:     "payments",
			Content:     "Owned by billing team",
			Source:      "clarification",
			ComponentID: "clar-1",
		})

		facts, err := s.Facts.GetByType(ctx, "team_ownership")
		if err != nil {
			t.Fatalf("GetByType() error = %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("GetByType() returned %d, want 1", len(facts))
		}
		if facts[0].ComponentID != "clar-1" {
			t.Errorf("ComponentID = %q, want %q", facts[0].ComponentID, "clar-1")
		}
	})

	t.Run("search", func(t *testing.T) {
		facts, err := s.Facts.Search(ctx, "credit card")
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("Search() returned %d, want 1", len(facts))
		}

		facts, err = s.Facts.Search(ctx, "payments")
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(facts) != 2 {
			t.Errorf("Search(payments) returned %d, want 2", len(facts))
		}
	})

	t.Run("delete", func(t *testing.T) {
		facts, _ := s.Facts.GetByType(ctx, "service_purpose")
		if len(facts) == 0 {
			t.Fatal("expected at least one fact")
		}

		if err := s.Facts.Delete(ctx, facts[0].ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		remaining, _ := s.Facts.GetByType(ctx, "service_purpose")
		if len(remaining) != 0 {
			t.Errorf("expected 0 after delete, got %d", len(remaining))
		}
	})
}

func TestComponentRepository_ListAfterSync(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	src := &store.Component{
		ID:     "sync-src",
		Type:   "prometheus",
		Name:   "Synced Prometheus",
		Config: json.RawMessage(`{"url":"http://prom:9090"}`),
	}
	if err := s.Components.Create(ctx, src); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Set sync status so last_sync_at is populated.
	if err := s.Components.UpdateSyncStatus(ctx, "sync-src", time.Now(), ""); err != nil {
		t.Fatalf("UpdateSyncStatus() error = %v", err)
	}

	// List — scanComponents should now hit the lastSyncAt.Valid branch.
	components, err := s.Components.List(ctx)
	if err != nil {
		t.Fatalf("List() after sync error = %v", err)
	}
	var found *store.Component
	for _, s := range components {
		if s.ID == "sync-src" {
			found = s
			break
		}
	}
	if found == nil {
		t.Fatal("List() did not return synced source")
	}
	if found.LastSyncAt == nil {
		t.Error("LastSyncAt should be set after UpdateSyncStatus")
	}

	// ListByType — also exercises scanComponents with lastSyncAt.Valid.
	byType, err := s.Components.ListByType(ctx, "prometheus")
	if err != nil {
		t.Fatalf("ListByType() after sync error = %v", err)
	}
	if len(byType) == 0 {
		t.Fatal("ListByType() returned empty after sync")
	}
	if byType[0].LastSyncAt == nil {
		t.Error("LastSyncAt should be set in ListByType result")
	}
}

func TestClarification_AnswerAlreadyAnswered(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	c := &store.Clarification{
		ID:       "clar-race",
		Type:     store.ClarificationNewService,
		Context:  json.RawMessage(`{}`),
		Question: "Race condition test?",
	}
	if err := s.Clarifications.Create(ctx, c); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// First answer succeeds.
	if err := s.Clarifications.Answer(ctx, "clar-race", "Yes", "user1"); err != nil {
		t.Fatalf("Answer() first call error = %v", err)
	}

	// Second answer on an already-answered clarification must return ErrAlreadyAnswered.
	err := s.Clarifications.Answer(ctx, "clar-race", "No", "user2")
	if err == nil {
		t.Fatal("Answer() second call: expected ErrAlreadyAnswered, got nil")
	}
	if !errors.Is(err, store.ErrAlreadyAnswered) {
		t.Errorf("Answer() second call error = %v, want ErrAlreadyAnswered", err)
	}
}

func TestForeignKeyEnforcement(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// session_messages should fail if session doesn't exist
	err := s.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: "nonexistent-session",
		Role:      "user",
		Content:   "hello",
	})
	if err == nil {
		t.Error("expected foreign key error for nonexistent session, got nil")
	}
}

func TestNewErrorHandling(t *testing.T) {
	t.Run("invalid database path", func(t *testing.T) {
		// Try to create a database in a non-existent directory
		s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: "/nonexistent/path/to/db.sqlite"}, nil)
		if err == nil {
			t.Error("expected error opening database in invalid path, got nil")
			if s != nil {
				s.Close()
			}
		}
	})
}

func TestMigrateErrorHandling(t *testing.T) {
	t.Run("migrate already migrated database", func(t *testing.T) {
		s := setupTestStore(t)
		// Run migrate again on already-migrated database
		err := s.Migrate()
		// Should not error since ErrNoChange is handled
		if err != nil {
			t.Errorf("Migrate() on already-migrated database returned error: %v", err)
		}
	})
}

func TestRepositoryErrorPaths(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	t.Run("clarification list by invalid status", func(t *testing.T) {
		// This should still work, just return empty list
		clarifications, err := s.Clarifications.ListByStatus(ctx, "invalid_status")
		if err != nil {
			t.Fatalf("ListByStatus() error = %v", err)
		}
		if len(clarifications) != 0 {
			t.Errorf("expected empty list for invalid status, got %d", len(clarifications))
		}
	})

	t.Run("cache operations with minimal parsed data", func(t *testing.T) {
		cache := &store.JoeFileCache{
			FilePath:    "/test/minimal",
			ContentHash: "minimal",
			ParsedData:  json.RawMessage(`{}`), // minimal valid JSON
		}
		if err := s.Cache.Set(ctx, cache); err != nil {
			t.Fatalf("Set() with minimal ParsedData error = %v", err)
		}
		got, err := s.Cache.Get(ctx, "/test/minimal")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got == nil {
			t.Error("expected non-nil result")
		}
		if string(got.ParsedData) != "{}" {
			t.Errorf("ParsedData = %q, want {}", string(got.ParsedData))
		}
	})

	t.Run("fact repository with empty component_id", func(t *testing.T) {
		fact := &store.OnboardingFact{
			FactType:    "test",
			Subject:     "test-subject",
			Content:     "test content",
			Source:      "test",
			ComponentID: "", // empty component_id
		}
		if err := s.Facts.Create(ctx, fact); err != nil {
			t.Fatalf("Create() with empty ComponentID error = %v", err)
		}
		facts, err := s.Facts.GetBySubject(ctx, "test-subject")
		if err != nil {
			t.Fatalf("GetBySubject() error = %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("expected 1 fact, got %d", len(facts))
		}
		if facts[0].ComponentID != "" {
			t.Errorf("expected empty ComponentID, got %q", facts[0].ComponentID)
		}
	})

	t.Run("session with metadata", func(t *testing.T) {
		session := &store.Session{ID: "sess-meta"}
		if err := s.Sessions.Create(ctx, session); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		metadata := json.RawMessage(`{"key": "value"}`)
		if err := s.Sessions.End(ctx, "sess-meta", "test summary", metadata); err != nil {
			t.Fatalf("End() with metadata error = %v", err)
		}

		got, err := s.Sessions.Get(ctx, "sess-meta")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Metadata == nil {
			t.Error("expected non-nil metadata")
		}
	})

	t.Run("source without last_sync_at", func(t *testing.T) {
		src := &store.Component{
			ID:     "test-src",
			Type:   "test",
			Name:   "Test Source",
			Config: json.RawMessage(`{}`),
		}
		if err := s.Components.Create(ctx, src); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := s.Components.Get(ctx, "test-src")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.LastSyncAt != nil {
			t.Error("expected nil LastSyncAt for new source")
		}
	})
}

func TestAllowedComponentTypes(t *testing.T) {
	types := store.AllowedComponentTypes()
	if len(types) == 0 {
		t.Fatal("AllowedComponentTypes() returned empty slice")
	}

	// Check a few well-known types are present.
	want := []string{"kubernetes", "git", "prometheus", "github"}
	found := make(map[string]bool, len(types))
	for _, tp := range types {
		found[tp] = true
	}
	for _, w := range want {
		if !found[w] {
			t.Errorf("AllowedComponentTypes() missing %q", w)
		}
	}
}

func TestIsValidComponentType(t *testing.T) {
	tests := []struct {
		sourceType string
		want       bool
	}{
		{"kubernetes", true},
		{"git", true},
		{"aws", true},
		{"prometheus", true},
		{"github", true},
		{"gitlab", true},
		{"unknown", false},
		{"", false},
		{"KUBERNETES", false},
	}
	for _, tt := range tests {
		t.Run(tt.sourceType, func(t *testing.T) {
			got := store.IsValidComponentType(tt.sourceType)
			if got != tt.want {
				t.Errorf("IsValidComponentType(%q) = %v, want %v", tt.sourceType, got, tt.want)
			}
		})
	}
}

func TestStore_Driver(t *testing.T) {
	s := setupTestStore(t)
	if got := s.Driver(); got != store.DriverSQLite {
		t.Errorf("Driver() = %q, want %q", got, store.DriverSQLite)
	}
}

func TestStore_PanicStore(t *testing.T) {
	s := setupTestStore(t)
	ps := s.PanicStore()
	if ps == nil {
		t.Fatal("PanicStore() returned nil")
	}
}

func TestPanicStore_StateTransitions(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	ps := s.PanicStore()

	// Initially not panicked.
	panicked, err := ps.IsPanicked(ctx)
	if err != nil {
		t.Fatalf("IsPanicked() error = %v", err)
	}
	if panicked {
		t.Error("IsPanicked() = true before SetPanicked, want false")
	}

	// Set panicked with trigger detail.
	if err := ps.SetPanicked(ctx, safety.PanicSourceCLI, "operator test"); err != nil {
		t.Fatalf("SetPanicked() error = %v", err)
	}

	panicked, err = ps.IsPanicked(ctx)
	if err != nil {
		t.Fatalf("IsPanicked() error after SetPanicked = %v", err)
	}
	if !panicked {
		t.Error("IsPanicked() = false after SetPanicked, want true")
	}

	// PanicInfo reports the recorded who/why from the same row.
	info, err := ps.PanicInfo(ctx)
	if err != nil {
		t.Fatalf("PanicInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("PanicInfo() = nil while panicked, want detail")
	}
	if info.TriggerSource != safety.PanicSourceCLI || info.TriggerReason != "operator test" {
		t.Errorf("PanicInfo() = %+v, want source=cli reason=%q", info, "operator test")
	}

	// Clear panicked.
	if err := ps.ClearPanicked(ctx); err != nil {
		t.Fatalf("ClearPanicked() error = %v", err)
	}

	panicked, err = ps.IsPanicked(ctx)
	if err != nil {
		t.Fatalf("IsPanicked() error after ClearPanicked = %v", err)
	}
	if panicked {
		t.Error("IsPanicked() = true after ClearPanicked, want false")
	}

	// PanicInfo reports nil once the row is cleared.
	info, err = ps.PanicInfo(ctx)
	if err != nil {
		t.Fatalf("PanicInfo() error after ClearPanicked = %v", err)
	}
	if info != nil {
		t.Errorf("PanicInfo() = %+v after clear, want nil", info)
	}
}

func TestCacheRepository_WithToolCallsAndProcessedAt(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	cache := &store.JoeFileCache{
		FilePath:    "/repo/.joe/toolcalls.md",
		ContentHash: "tc123",
		ParsedData:  json.RawMessage(`{"service":"payments"}`),
		ToolCalls:   json.RawMessage(`[{"name":"graph_query","args":{}}]`),
		ProcessedAt: now,
	}
	if err := s.Cache.Set(ctx, cache); err != nil {
		t.Fatalf("Set() with ToolCalls error = %v", err)
	}

	got, err := s.Cache.Get(ctx, "/repo/.joe/toolcalls.md")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if string(got.ToolCalls) != `[{"name":"graph_query","args":{}}]` {
		t.Errorf("ToolCalls = %s, want original value", got.ToolCalls)
	}
	if got.ProcessedAt.IsZero() {
		t.Error("ProcessedAt should not be zero")
	}
}

func TestClarification_WithGraphOperations(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	graphOps := json.RawMessage(`[{"type":"add_edge","from":"svc-a","to":"svc-b"}]`)
	c := &store.Clarification{
		ID:              "clar-graphops",
		Type:            store.ClarificationEdgeConfirm,
		Context:         json.RawMessage(`{"service":"svc-a"}`),
		Question:        "Does svc-a depend on svc-b?",
		GraphOperations: graphOps,
	}
	if err := s.Clarifications.Create(ctx, c); err != nil {
		t.Fatalf("Create() with GraphOperations error = %v", err)
	}

	got, err := s.Clarifications.Get(ctx, "clar-graphops")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if string(got.GraphOperations) != string(graphOps) {
		t.Errorf("GraphOperations = %s, want %s", got.GraphOperations, graphOps)
	}
}

func TestCloseStore(t *testing.T) {
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Close should work
	if err := s.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Operations after close should fail
	ctx := context.Background()
	_, err = s.Components.List(ctx)
	if err == nil {
		t.Error("expected error after Close(), got nil")
	}
}
