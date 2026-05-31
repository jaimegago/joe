package llmusage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// ErrUsageWriteFailed wraps a lower-level error so callers can identify a
// usage-recording failure without depending on the underlying driver error
// shape. The recorder tests usage errors via errors.Is(err,
// ErrUsageWriteFailed) when asserting fail-open behaviour.
var ErrUsageWriteFailed = errors.New("llmusage: write failed")

// TimestampLayout is the canonical UTC layout for llm_usage.created_at and
// every boundary string compared against it. The fractional-second segment
// is fixed at nine digits with leading zeros preserved so EVERY formatted
// timestamp has the same byte length and byte-wise lexicographic order
// agrees with chronological order across every pair — including pairs that
// straddle a whole-second boundary (which RFC3339Nano's trailing-zero
// trimming silently inverts, since '.' (0x2E) sorts below 'Z' (0x5A)).
//
// Range queries on idx_llm_usage_created_at must format their lower and
// upper bound strings with this same layout; using RFC3339Nano on a
// boundary would re-introduce the same monotonicity break.
//
// The column type stays TEXT (migration 017) — this is a write-side
// formatting decision only.
const TimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Row is one llm_usage record. Field names mirror the columns added by
// migration 017. Timestamp is filled by the repository when zero; the
// recorder leaves it zero so the SQL store stamps the canonical UTC
// value formatted with TimestampLayout (fixed-width nanosecond) so
// lexicographic order over created_at matches chronological order.
//
// Principal, SessionID, and TaskID are stored as SQL NULL when empty —
// the migration 017 columns are nullable for exactly this case, matching
// audit_log's convention (every empty string round-trips to NULL).
// Currency is NOT NULL with no default; callers must always supply it.
type Row struct {
	Timestamp         time.Time
	Principal         string
	Model             string
	InputTokens       int
	OutputTokens      int
	EstimatedCostNano int64
	Currency          string
	SessionID         string
	TaskID            string
}

// Repository is the read+insert surface for llm_usage. Like audit, there
// is no Update, Delete, or Truncate method — usage rows are an
// observability log, and the recorder is fail-open against the write
// side, so Insert is all the recording path ever needs.
//
// SumCostNano is the read primitive added in Stream G phase G3b for the
// pre-call cost-window gate: it returns the exact integer sum of
// estimated_cost_nano for rows whose created_at falls in the half-open
// range [lower, upper) AND whose currency equals the supplied filter.
// The currency filter is locked Rule 4 — nano-units of different
// currencies cannot be added, so the gate sums only same-currency rows.
type Repository interface {
	Insert(ctx context.Context, r Row) error
	SumCostNano(ctx context.Context, lower, upper time.Time, currency string) (int64, error)
	// CountForeignCurrency returns the count of rows whose currency is
	// NOT equal to the supplied currency. Used by the once-only
	// mixed-currency detector at recorder construction — never on the
	// per-call enforcement path. A non-zero result is the fingerprint of
	// an operator who changed the configured currency between
	// deployments, leaving rows in two denominations in the same table.
	CountForeignCurrency(ctx context.Context, currency string) (int64, error)
}

// NewRepository builds the SQL-backed Repository for the given database
// handle and driver name (store.DriverSQLite or store.DriverPostgres).
// The driver argument is forwarded to store.Rebind so the same statement
// works against both engines without per-call branching.
func NewRepository(db *sql.DB, driver string) Repository {
	return &sqlRepository{db: db, driver: driver}
}

type sqlRepository struct {
	db     *sql.DB
	driver string
}

func (r *sqlRepository) Insert(ctx context.Context, row Row) error {
	if row.Timestamp.IsZero() {
		row.Timestamp = time.Now().UTC()
	}
	if row.Currency == "" {
		// Currency is NOT NULL on the column. Refuse the row rather than
		// silently stamping a wrong value — every recorder construction
		// site supplies a configured currency, so an empty value here is
		// a wiring bug.
		return fmt.Errorf("%w: currency required", ErrUsageWriteFailed)
	}
	if row.Model == "" {
		return fmt.Errorf("%w: model required", ErrUsageWriteFailed)
	}

	var (
		principal sql.NullString
		sessionID sql.NullString
		taskID    sql.NullString
	)
	if row.Principal != "" {
		principal = sql.NullString{String: row.Principal, Valid: true}
	}
	if row.SessionID != "" {
		sessionID = sql.NullString{String: row.SessionID, Valid: true}
	}
	if row.TaskID != "" {
		taskID = sql.NullString{String: row.TaskID, Valid: true}
	}

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO llm_usage
			(created_at, principal, model, input_tokens, output_tokens, estimated_cost_nano, currency, session_id, task_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		row.Timestamp.UTC().Format(TimestampLayout),
		principal,
		row.Model,
		row.InputTokens,
		row.OutputTokens,
		row.EstimatedCostNano,
		row.Currency,
		sessionID,
		taskID,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUsageWriteFailed, err)
	}
	return nil
}

// SumCostNano runs the half-open range query
//
//	SELECT COALESCE(SUM(estimated_cost_nano), 0)
//	  FROM llm_usage
//	 WHERE created_at >= ? AND created_at < ?
//	   AND currency = ?
//
// over the idx_llm_usage_created_at index. The time bounds are formatted
// here, NOT at the call site, with TimestampLayout — the layout invariant
// has exactly one enforcement point on the read path so a future caller
// cannot accidentally re-introduce RFC3339Nano's trailing-zero trimming
// (which silently inverts lex order across whole-second boundaries; see
// the layout's package doc). COALESCE returns zero on an empty window so
// the gate's comparison code does not have to handle NULL.
//
// The currency filter is the locked Rule 4: nano-units of different
// currencies are not addable, so the recorder gate sums only rows
// matching the configured currency. Mixed-currency detection is a
// SEPARATE, once-only path (CountForeignCurrency) — never per-call.
func (r *sqlRepository) SumCostNano(ctx context.Context, lower, upper time.Time, currency string) (int64, error) {
	var sum int64
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT COALESCE(SUM(estimated_cost_nano), 0)
		  FROM llm_usage
		 WHERE created_at >= ? AND created_at < ?
		   AND currency = ?`),
		lower.UTC().Format(TimestampLayout),
		upper.UTC().Format(TimestampLayout),
		currency,
	).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("llmusage: sum cost: %w", err)
	}
	return sum, nil
}

// CountForeignCurrency counts rows whose currency differs from the
// configured currency. Cold path — invoked once at recorder
// construction (or first use) by the mixed-currency detector. NEVER
// called on the per-call enforcement path; the gate never needs to
// count foreign rows, only sum same-currency rows.
func (r *sqlRepository) CountForeignCurrency(ctx context.Context, currency string) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT COUNT(*) FROM llm_usage WHERE currency <> ?`),
		currency,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("llmusage: count foreign currency: %w", err)
	}
	return n, nil
}
