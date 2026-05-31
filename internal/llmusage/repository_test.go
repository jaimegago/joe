package llmusage_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

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

// TestTimestampLayout_LexMatchesChronology asserts the layout produces
// fixed-width strings whose byte-wise order matches chronological order
// across whole-second boundaries. With the pre-fix RFC3339Nano layout
// the +500ms timestamp formatted to "12:00:00.5Z" and lex-sorted BELOW
// the whole-second "12:00:00Z" boundary string because '.' (0x2E) sorts
// below 'Z' (0x5A). The fixed-width layout pads the fractional segment
// to nine digits so '.' is always present and the comparison is sound.
func TestTimestampLayout_LexMatchesChronology(t *testing.T) {
	whole := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	half := time.Date(2026, 1, 1, 12, 0, 0, 500_000_000, time.UTC)
	wholeStr := whole.Format(llmusage.TimestampLayout)
	halfStr := half.Format(llmusage.TimestampLayout)

	if len(wholeStr) != len(halfStr) {
		t.Fatalf("formatted timestamps have differing lengths: whole=%d half=%d (whole=%q, half=%q)",
			len(wholeStr), len(halfStr), wholeStr, halfStr)
	}
	if !(wholeStr < halfStr) {
		t.Fatalf("lex order disagrees with chronology: whole=%q halfPlus=%q (want whole < half)",
			wholeStr, halfStr)
	}

	// Differing-precision pairs (a property that mattered for RFC3339Nano)
	// are not produceable with this layout since width is fixed, but the
	// equivalent assertion — that a sub-microsecond difference still
	// sorts correctly — is verified here.
	finerA := time.Date(2026, 1, 1, 12, 0, 0, 1, time.UTC).Format(llmusage.TimestampLayout)
	finerB := time.Date(2026, 1, 1, 12, 0, 0, 2, time.UTC).Format(llmusage.TimestampLayout)
	if !(finerA < finerB) {
		t.Fatalf("lex order disagrees with chronology at 1ns granularity: a=%q b=%q", finerA, finerB)
	}
}

// TestSQLRepository_HalfOpenRange_SubSecondCluster is the empirical
// pre-G3 regression: insert two rows whose created_at differ only in
// sub-second fraction — one at a whole-second instant and one 500ms
// later — both at-or-after the whole-second lower bound and before the
// upper bound, then run the half-open range query the cost-window
// aggregation will use, with the lower bound formatted by the same
// llmusage.TimestampLayout constant. Both rows must be returned and a
// SUM over estimated_cost_nano must equal the sum of both. Against the
// pre-fix RFC3339Nano write format this fails by returning one row.
func TestSQLRepository_HalfOpenRange_SubSecondCluster(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	boundary := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) // inclusive lower
	later := boundary.Add(500 * time.Millisecond)            // same window, +500ms
	end := boundary.Add(time.Hour)                           // exclusive upper

	if err := repo.Insert(context.Background(), llmusage.Row{
		Timestamp:         boundary,
		Model:             "claude-sonnet-4-20250514",
		EstimatedCostNano: 3,
		Currency:          "USD",
	}); err != nil {
		t.Fatalf("insert boundary: %v", err)
	}
	if err := repo.Insert(context.Background(), llmusage.Row{
		Timestamp:         later,
		Model:             "claude-sonnet-4-20250514",
		EstimatedCostNano: 5,
		Currency:          "USD",
	}); err != nil {
		t.Fatalf("insert later: %v", err)
	}

	lowerBound := boundary.Format(llmusage.TimestampLayout)
	upperBound := end.Format(llmusage.TimestampLayout)

	var count int
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM llm_usage WHERE created_at >= ? AND created_at < ?`,
		lowerBound, upperBound,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("range query returned %d rows; want 2 (lower=%q upper=%q)",
			count, lowerBound, upperBound)
	}

	var sum int64
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT COALESCE(SUM(estimated_cost_nano), 0) FROM llm_usage WHERE created_at >= ? AND created_at < ?`,
		lowerBound, upperBound,
	).Scan(&sum); err != nil {
		t.Fatalf("sum: %v", err)
	}
	if sum != 8 {
		t.Errorf("SUM(estimated_cost_nano) = %d; want 8 (3 boundary + 5 later)", sum)
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
