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

// Repository is the insert-only surface for llm_usage. Like audit, there
// is no Update, Delete, or Truncate method — usage rows are an
// observability log, and the recorder is fail-open against this
// interface, so all the caller ever needs is Insert.
type Repository interface {
	Insert(ctx context.Context, r Row) error
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
