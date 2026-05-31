package llmusage_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/store"
)

// freshStore opens an in-memory SQLite with the full migration chain
// applied. The audit package uses the same helper shape; we duplicate
// it here so the usage tests don't reach into a sibling _test package.
func freshStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestSQLRepository_Insert_RoundTrip writes one row and reads it back
// via raw SELECT. Exercises the migration-017 column types and the
// repository's empty-string-to-NULL convention.
func TestSQLRepository_Insert_RoundTrip(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), llmusage.Row{
		Principal:         "user:alice",
		Model:             "claude-sonnet-4-20250514",
		InputTokens:       100,
		OutputTokens:      50,
		EstimatedCostNano: 1_500_000,
		Currency:          "USD",
		SessionID:         "sess-1",
		TaskID:            "task-1",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var (
		principal sql.NullString
		model     string
		inTok     int
		outTok    int
		cost      int64
		currency  string
		sessID    sql.NullString
		taskID    sql.NullString
	)
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT principal, model, input_tokens, output_tokens, estimated_cost_nano, currency, session_id, task_id
		   FROM llm_usage LIMIT 1`).
		Scan(&principal, &model, &inTok, &outTok, &cost, &currency, &sessID, &taskID)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if !principal.Valid || principal.String != "user:alice" {
		t.Errorf("principal = %+v, want valid user:alice", principal)
	}
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q", model)
	}
	if inTok != 100 || outTok != 50 {
		t.Errorf("tokens (in/out) = %d/%d, want 100/50", inTok, outTok)
	}
	if cost != 1_500_000 {
		t.Errorf("cost = %d, want 1_500_000 nano-units", cost)
	}
	if currency != "USD" {
		t.Errorf("currency = %q, want USD", currency)
	}
	if !sessID.Valid || sessID.String != "sess-1" {
		t.Errorf("session_id = %+v", sessID)
	}
	if !taskID.Valid || taskID.String != "task-1" {
		t.Errorf("task_id = %+v", taskID)
	}
}

// TestSQLRepository_Insert_EmptyToNull — principal, session_id, and
// task_id are nullable on the migration-017 column. Empty Go strings
// must round-trip to SQL NULL so consumers' COUNT-by-principal
// queries don't see an empty string as a distinct value.
func TestSQLRepository_Insert_EmptyToNull(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	if err := repo.Insert(context.Background(), llmusage.Row{
		Model:    "claude-sonnet-4-20250514",
		Currency: "USD",
		// Principal, SessionID, TaskID intentionally empty.
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var principal, sessID, taskID sql.NullString
	err := s.DB().QueryRowContext(context.Background(),
		`SELECT principal, session_id, task_id FROM llm_usage LIMIT 1`).
		Scan(&principal, &sessID, &taskID)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if principal.Valid {
		t.Errorf("principal stored as %q; want NULL for empty-string input", principal.String)
	}
	if sessID.Valid {
		t.Errorf("session_id stored as %q; want NULL", sessID.String)
	}
	if taskID.Valid {
		t.Errorf("task_id stored as %q; want NULL", taskID.String)
	}
}

// TestSQLRepository_Insert_EmptyCurrencyRejected — the currency
// column is NOT NULL with no default, so the recorder must always
// supply it. An empty Currency at the repository boundary is a wiring
// bug, not a row to silently default.
func TestSQLRepository_Insert_EmptyCurrencyRejected(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	err := repo.Insert(context.Background(), llmusage.Row{
		Model: "claude-sonnet-4-20250514",
		// Currency intentionally empty.
	})
	if err == nil {
		t.Fatal("Insert with empty currency returned nil; want an error")
	}
	if !strings.Contains(err.Error(), "currency") {
		t.Errorf("error = %v; want a message mentioning currency", err)
	}
}
