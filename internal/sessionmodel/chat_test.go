package sessionmodel_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// TestRepository_VisibilityDefault verifies CreateSession defaults visibility to
// 'private' when the caller leaves it unset (migration 022).
func TestRepository_VisibilityDefault(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	created, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeOther,
		CreatorPrincipal: "user:alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.Visibility != sessionmodel.VisibilityPrivate {
		t.Errorf("returned visibility = %q, want %q", created.Visibility, sessionmodel.VisibilityPrivate)
	}
	got, err := repo.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Visibility != sessionmodel.VisibilityPrivate {
		t.Errorf("stored visibility = %q, want %q", got.Visibility, sessionmodel.VisibilityPrivate)
	}
}

// TestRepository_ListSessionsByCreator verifies owner-scoping, recency ordering,
// per-session message counts, and the limit.
func TestRepository_ListSessionsByCreator(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	// Explicit, well-separated timestamps so recency ordering is deterministic:
	// last_activity_at is RFC3339 (second precision), so sessions created within
	// the same second would otherwise tie.
	base := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	mk := func(id, principal string, at time.Time) {
		if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeOther, CreatorPrincipal: principal,
			CreatedAt: at, LastActivityAt: at,
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", id, err)
		}
	}
	mk("a1", "user:alice@example.com", base.Add(2*time.Minute)) // most recent creation
	mk("a2", "user:alice@example.com", base)
	mk("b1", "user:bob@example.com", base.Add(1*time.Minute))

	// Two messages on a2 bump its last_activity_at past a1's, so a2 sorts first.
	for _, m := range []sessionmodel.ChatMessage{
		{ID: "m1", SessionID: "a2", Role: "user", Content: "q", CreatedAt: base.Add(5 * time.Minute)},
		{ID: "m2", SessionID: "a2", Role: "assistant", Content: "a", CreatedAt: base.Add(6 * time.Minute)},
	} {
		if _, err := repo.AddChatMessage(ctx, m); err != nil {
			t.Fatalf("AddChatMessage: %v", err)
		}
	}

	alice, err := repo.ListSessionsByCreator(ctx, "user:alice@example.com", 0)
	if err != nil {
		t.Fatalf("ListSessionsByCreator: %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("alice sees %d sessions, want 2 (bob's must not leak)", len(alice))
	}
	// a2 has more recent activity (a message bump), so it sorts first.
	if alice[0].ID != "a2" {
		t.Errorf("first session = %q, want a2 (most recent activity)", alice[0].ID)
	}
	if alice[0].MessageCount != 2 {
		t.Errorf("a2 message_count = %d, want 2", alice[0].MessageCount)
	}
	if alice[1].MessageCount != 0 {
		t.Errorf("a1 message_count = %d, want 0", alice[1].MessageCount)
	}

	// Limit is honored.
	limited, err := repo.ListSessionsByCreator(ctx, "user:alice@example.com", 1)
	if err != nil {
		t.Fatalf("ListSessionsByCreator limit: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limit=1 returned %d sessions", len(limited))
	}
}

// TestRepository_UpdateSessionTitle verifies a rename persists and that it does
// not bump last_activity_at (a rename is metadata, not chat activity, so it must
// not reorder the recency-sorted browse list).
func TestRepository_UpdateSessionTitle(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	at := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "s1", Type: sessionmodel.SessionTypeOther, CreatorPrincipal: "user:alice@example.com",
		CreatedAt: at, LastActivityAt: at,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := repo.UpdateSessionTitle(ctx, "s1", "Postgres Connection Pool"); err != nil {
		t.Fatalf("UpdateSessionTitle: %v", err)
	}

	got, err := repo.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Title == nil || *got.Title != "Postgres Connection Pool" {
		t.Errorf("title = %v, want %q", got.Title, "Postgres Connection Pool")
	}
	// last_activity_at must be unchanged by a rename.
	if !got.LastActivityAt.Equal(at) {
		t.Errorf("last_activity_at = %v, want unchanged %v", got.LastActivityAt, at)
	}
}

// TestRepository_ChatMessages verifies seq assignment, ordering, and that a
// session DELETE cascades its messages away (§6-C expunge).
func TestRepository_ChatMessages(t *testing.T) {
	s := newTestStore(t)
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	if _, err := repo.CreateSession(ctx, sessionmodel.AgentSession{
		ID: "sess", Type: sessionmodel.SessionTypeOther, CreatorPrincipal: "user:alice@example.com",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	first, err := repo.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID: "m1", SessionID: "sess", Role: "user", Content: "one",
	})
	if err != nil {
		t.Fatalf("AddChatMessage 1: %v", err)
	}
	if first.Seq != 1 {
		t.Errorf("first seq = %d, want 1", first.Seq)
	}
	second, err := repo.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID: "m2", SessionID: "sess", Role: "assistant", Content: "two",
	})
	if err != nil {
		t.Fatalf("AddChatMessage 2: %v", err)
	}
	if second.Seq != 2 {
		t.Errorf("second seq = %d, want 2", second.Seq)
	}

	msgs, err := repo.ListChatMessages(ctx, "sess")
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Content != "one" || msgs[1].Content != "two" {
		t.Fatalf("messages = %+v, want [one, two] in order", msgs)
	}

	// Deleting the session cascades its messages (FK ON DELETE CASCADE).
	if err := repo.DeleteSession(ctx, "sess"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	after, err := repo.ListChatMessages(ctx, "sess")
	if err != nil {
		t.Fatalf("ListChatMessages after delete: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("messages after session delete = %d, want 0 (cascade)", len(after))
	}
}
