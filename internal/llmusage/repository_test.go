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

// TestSQLRepository_SumCostNano_SubSecondCluster is the regression
// guard at the AGGREGATION site for the fixed-width timestamp invariant.
// The half-open repository SELECT exists in two forms — the raw SELECT
// the prior test ran (TestSQLRepository_HalfOpenRange_SubSecondCluster)
// and the SumCostNano method the cost-window gate uses. This test
// exercises the method itself so a future change that bypasses
// TimestampLayout in SumCostNano (and uses RFC3339Nano directly) fails
// here, not silently in production aggregate undercount.
func TestSQLRepository_SumCostNano_SubSecondCluster(t *testing.T) {
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

	sum, err := repo.SumCostNano(context.Background(), boundary, end, "USD")
	if err != nil {
		t.Fatalf("SumCostNano: %v", err)
	}
	if sum != 8 {
		t.Errorf("SumCostNano = %d, want 8 (3 boundary + 5 later) — fixed-width timestamp invariant broken at aggregation site",
			sum)
	}
}

// TestSQLRepository_SumCostNano_SameCurrencyFilter pins the locked
// Rule 4: nano-units of different currencies cannot be added, so the
// gate's aggregation MUST exclude rows in any currency other than the
// supplied filter. Two rows in the same window, one USD and one EUR,
// must sum to only the USD row's cost when summing in USD.
func TestSQLRepository_SumCostNano_SameCurrencyFilter(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	boundary := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	end := boundary.Add(time.Hour)
	mid := boundary.Add(10 * time.Minute)

	if err := repo.Insert(context.Background(), llmusage.Row{
		Timestamp: boundary, Model: "claude-sonnet-4-20250514",
		EstimatedCostNano: 100, Currency: "USD",
	}); err != nil {
		t.Fatalf("insert USD: %v", err)
	}
	if err := repo.Insert(context.Background(), llmusage.Row{
		Timestamp: mid, Model: "claude-sonnet-4-20250514",
		EstimatedCostNano: 999_999_999, Currency: "EUR",
	}); err != nil {
		t.Fatalf("insert EUR: %v", err)
	}

	sumUSD, err := repo.SumCostNano(context.Background(), boundary, end, "USD")
	if err != nil {
		t.Fatalf("sum USD: %v", err)
	}
	if sumUSD != 100 {
		t.Errorf("USD sum = %d, want 100 — EUR row must NOT be added", sumUSD)
	}

	sumEUR, err := repo.SumCostNano(context.Background(), boundary, end, "EUR")
	if err != nil {
		t.Fatalf("sum EUR: %v", err)
	}
	if sumEUR != 999_999_999 {
		t.Errorf("EUR sum = %d, want 999_999_999", sumEUR)
	}
}

// TestSQLRepository_SumCostNano_EmptyWindowIsZero asserts COALESCE
// returns zero for a window containing no rows — the gate's comparison
// code does not need to handle NULL.
func TestSQLRepository_SumCostNano_EmptyWindowIsZero(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	lower := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	upper := lower.Add(time.Hour)
	sum, err := repo.SumCostNano(context.Background(), lower, upper, "USD")
	if err != nil {
		t.Fatalf("SumCostNano on empty table: %v", err)
	}
	if sum != 0 {
		t.Errorf("sum = %d, want 0 (COALESCE on empty window)", sum)
	}
}

// TestSQLRepository_CountForeignCurrency exercises the once-only
// detector's read primitive: rows whose currency differs from the
// supplied filter are counted; same-currency rows are not.
func TestSQLRepository_CountForeignCurrency(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)

	for _, cur := range []string{"USD", "USD", "EUR", "GBP"} {
		if err := repo.Insert(context.Background(), llmusage.Row{
			Model: "claude-sonnet-4-20250514", Currency: cur,
		}); err != nil {
			t.Fatalf("insert %s: %v", cur, err)
		}
	}
	n, err := repo.CountForeignCurrency(context.Background(), "USD")
	if err != nil {
		t.Fatalf("CountForeignCurrency: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2 (1 EUR + 1 GBP)", n)
	}
}

// TestSQLRepository_SessionUsage_GroupsByCurrency — Stream G phase
// G5. The session view groups by currency so a session whose rows
// span two currencies (operator changed the configured currency
// mid-session) renders as TWO rows, never one summed-across-
// currencies row that would violate locked Rule 4.
func TestSQLRepository_SessionUsage_GroupsByCurrency(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	for i, row := range []llmusage.Row{
		{Model: "m", Currency: "USD", EstimatedCostNano: 100, SessionID: "sess"},
		{Model: "m", Currency: "USD", EstimatedCostNano: 200, SessionID: "sess"},
		{Model: "m", Currency: "EUR", EstimatedCostNano: 50, SessionID: "sess"},
		{Model: "m", Currency: "USD", EstimatedCostNano: 999, SessionID: "other"},
	} {
		row.Timestamp = time.Date(2026, 1, 1, 12, i, 0, 0, time.UTC)
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	got, err := repo.SessionUsage(ctx, "sess")
	if err != nil {
		t.Fatalf("SessionUsage: %v", err)
	}
	byCur := map[string]llmusage.UsageBreakdown{}
	for _, r := range got {
		byCur[r.Currency] = r
	}
	if usd, ok := byCur["USD"]; !ok || usd.EstimatedCostNano != 300 || usd.Calls != 2 {
		t.Errorf("USD row = %+v; want 2 calls / 300 cost", usd)
	}
	if eur, ok := byCur["EUR"]; !ok || eur.EstimatedCostNano != 50 || eur.Calls != 1 {
		t.Errorf("EUR row = %+v; want 1 call / 50 cost", eur)
	}
	for _, r := range got {
		if r.SessionID != "sess" {
			t.Errorf("row carries wrong session id: %+v", r)
		}
	}
}

// TestSQLRepository_AggregateUsage_GroupsByCurrencyOverRange exercises
// the aggregate view's window filter + currency-grouping. Two rows
// inside the window in different currencies → two breakdown rows.
// A row outside the window must be excluded.
func TestSQLRepository_AggregateUsage_GroupsByCurrencyOverRange(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	lower := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	upper := lower.Add(time.Hour)
	for i, row := range []llmusage.Row{
		{Model: "m", Currency: "USD", EstimatedCostNano: 100, Timestamp: lower.Add(10 * time.Minute)},
		{Model: "m", Currency: "USD", EstimatedCostNano: 200, Timestamp: lower.Add(20 * time.Minute)},
		{Model: "m", Currency: "EUR", EstimatedCostNano: 75, Timestamp: lower.Add(30 * time.Minute)},
		{Model: "m", Currency: "USD", EstimatedCostNano: 999, Timestamp: upper.Add(time.Minute)}, // outside
	} {
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	got, err := repo.AggregateUsage(ctx, lower, upper)
	if err != nil {
		t.Fatalf("AggregateUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d; want 2 (USD + EUR)", len(got))
	}
	byCur := map[string]llmusage.UsageBreakdown{}
	for _, r := range got {
		byCur[r.Currency] = r
	}
	if usd := byCur["USD"]; usd.EstimatedCostNano != 300 || usd.Calls != 2 {
		t.Errorf("USD row = %+v; want 300 cost / 2 calls", usd)
	}
	if eur := byCur["EUR"]; eur.EstimatedCostNano != 75 || eur.Calls != 1 {
		t.Errorf("EUR row = %+v; want 75 cost / 1 call", eur)
	}
}

// TestSQLRepository_PerPrincipalUsage_NullPrincipalToEmpty — a row
// with no principal (anonymous/auth-disabled) must round-trip to an
// empty Principal string in the result so the HTTP renderer can
// render "anonymous" rather than fabricate a value.
func TestSQLRepository_PerPrincipalUsage_NullPrincipalToEmpty(t *testing.T) {
	s := freshStore(t)
	repo := llmusage.NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()

	lower := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	upper := lower.Add(time.Hour)
	if err := repo.Insert(ctx, llmusage.Row{
		Model: "m", Currency: "USD", EstimatedCostNano: 100,
		Timestamp: lower.Add(10 * time.Minute),
		// Principal intentionally empty → stored as NULL.
	}); err != nil {
		t.Fatalf("insert anonymous: %v", err)
	}
	if err := repo.Insert(ctx, llmusage.Row{
		Model: "m", Currency: "USD", EstimatedCostNano: 200,
		Timestamp: lower.Add(15 * time.Minute),
		Principal: "user:alice",
	}); err != nil {
		t.Fatalf("insert alice: %v", err)
	}

	got, err := repo.PerPrincipalUsage(ctx, lower, upper)
	if err != nil {
		t.Fatalf("PerPrincipalUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d; want 2 (one empty, one user:alice)", len(got))
	}
	byPrincipal := map[string]llmusage.UsageBreakdown{}
	for _, r := range got {
		byPrincipal[r.Principal] = r
	}
	if anon, ok := byPrincipal[""]; !ok || anon.EstimatedCostNano != 100 {
		t.Errorf("anonymous row = %+v; want empty-principal 100 cost", anon)
	}
	if alice, ok := byPrincipal["user:alice"]; !ok || alice.EstimatedCostNano != 200 {
		t.Errorf("alice row = %+v; want 200 cost", alice)
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
