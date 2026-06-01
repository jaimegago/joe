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

// UsageBreakdown is one row of an aggregated usage report. The view
// methods (SessionUsage / AggregateUsage / PerModelUsage /
// PerPrincipalUsage) return slices of these so the HTTP layer can
// render columnar tables WITHOUT a second round-trip per row. Every
// breakdown carries Currency so amounts (EstimatedCostNano in
// nano-units of the row's currency) can be labelled correctly — a
// table that summed across currencies would violate the locked Rule 4
// the cost-window gate also obeys.
//
// Model and Principal are populated only by the breakdown variants
// that group on them. PerModelUsage fills Model, PerPrincipalUsage
// fills Principal, the session and overall-aggregate variants leave
// both empty. SessionID is filled only by SessionUsage. A consumer
// that needs to know which grouping it is reading does so by which
// method it called, not by sniffing the row fields.
type UsageBreakdown struct {
	Calls             int64
	InputTokens       int64
	OutputTokens      int64
	EstimatedCostNano int64
	Currency          string
	Model             string
	Principal         string
	SessionID         string
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
//
// The four breakdown methods below are the Stream G phase G5 read-only
// VIEW path, separate from the enforcement SumCostNano: the gate
// returns one scalar to decide pass/deny, the views return rows to
// render an admin or operator UI. Splitting them keeps the
// enforcement query small (one COALESCE-SUM over an indexed range,
// hot path) and the display queries' GROUP BYs out of the
// per-call path. A consumer that wants to render a UI MUST go through
// the views; reusing SumCostNano on the display path would force every
// future shape change (added columns, a different group) to also touch
// the gate.
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
	// SessionUsage returns one row per currency for the given session
	// id (the SQL NULL principal/model/task columns are ignored — only
	// rows whose session_id matches participate). An empty result for
	// an unknown id is not an error; the caller decides whether to map
	// to 404. Stream G phase G5.
	SessionUsage(ctx context.Context, sessionID string) ([]UsageBreakdown, error)
	// AggregateUsage returns one row per currency over the half-open
	// time range [lower, upper). The HTTP layer calls it three times
	// (today, this week, this month) to assemble the dashboard summary.
	// Stream G phase G5.
	AggregateUsage(ctx context.Context, lower, upper time.Time) ([]UsageBreakdown, error)
	// PerModelUsage returns one row per (model, currency) pair over
	// [lower, upper). Rows with the same currency are added; rows in
	// different currencies are emitted as separate breakdown rows so
	// amounts remain comparable within a row. Stream G phase G5.
	PerModelUsage(ctx context.Context, lower, upper time.Time) ([]UsageBreakdown, error)
	// PerPrincipalUsage returns one row per (principal, currency)
	// pair over [lower, upper). NULL principals (anonymous /
	// auth-disabled rows) round-trip to an empty Principal string in
	// the result; the HTTP handler decides how to render them. The
	// endpoint that serves this method is admin-gated — the breakdown
	// reveals per-user spending patterns, which non-admins should not
	// see. Stream G phase G5.
	PerPrincipalUsage(ctx context.Context, lower, upper time.Time) ([]UsageBreakdown, error)
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

// SessionUsage returns one breakdown row per currency for the given
// session id, summing tokens / cost across every row that carries the
// session_id. Empty result for an unknown id is a normal outcome (no
// error). NULL session_id rows are excluded by the WHERE clause.
func (r *sqlRepository) SessionUsage(ctx context.Context, sessionID string) ([]UsageBreakdown, error) {
	if sessionID == "" {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(estimated_cost_nano), 0),
			currency
		FROM llm_usage
		WHERE session_id = ?
		GROUP BY currency
		ORDER BY currency`),
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("llmusage: session usage: %w", err)
	}
	defer rows.Close()
	var out []UsageBreakdown
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Calls, &b.InputTokens, &b.OutputTokens, &b.EstimatedCostNano, &b.Currency); err != nil {
			return nil, fmt.Errorf("llmusage: scan session usage: %w", err)
		}
		b.SessionID = sessionID
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("llmusage: iter session usage: %w", err)
	}
	return out, nil
}

// AggregateUsage returns one row per currency over the half-open
// [lower, upper) range. Bounds are formatted with TimestampLayout
// (same fixed-width layout the gate uses) so the comparison agrees
// with the write-time format on the index.
func (r *sqlRepository) AggregateUsage(ctx context.Context, lower, upper time.Time) ([]UsageBreakdown, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(estimated_cost_nano), 0),
			currency
		FROM llm_usage
		WHERE created_at >= ? AND created_at < ?
		GROUP BY currency
		ORDER BY currency`),
		lower.UTC().Format(TimestampLayout),
		upper.UTC().Format(TimestampLayout),
	)
	if err != nil {
		return nil, fmt.Errorf("llmusage: aggregate usage: %w", err)
	}
	defer rows.Close()
	var out []UsageBreakdown
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Calls, &b.InputTokens, &b.OutputTokens, &b.EstimatedCostNano, &b.Currency); err != nil {
			return nil, fmt.Errorf("llmusage: scan aggregate usage: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("llmusage: iter aggregate usage: %w", err)
	}
	return out, nil
}

// PerModelUsage returns one row per (model, currency) over [lower,
// upper). Rows in different currencies for the same model are emitted
// as separate breakdown rows so a consumer never adds nano-units
// across currencies (locked Rule 4).
func (r *sqlRepository) PerModelUsage(ctx context.Context, lower, upper time.Time) ([]UsageBreakdown, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT
			model,
			currency,
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(estimated_cost_nano), 0)
		FROM llm_usage
		WHERE created_at >= ? AND created_at < ?
		GROUP BY model, currency
		ORDER BY model, currency`),
		lower.UTC().Format(TimestampLayout),
		upper.UTC().Format(TimestampLayout),
	)
	if err != nil {
		return nil, fmt.Errorf("llmusage: per-model usage: %w", err)
	}
	defer rows.Close()
	var out []UsageBreakdown
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Model, &b.Currency, &b.Calls, &b.InputTokens, &b.OutputTokens, &b.EstimatedCostNano); err != nil {
			return nil, fmt.Errorf("llmusage: scan per-model usage: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("llmusage: iter per-model usage: %w", err)
	}
	return out, nil
}

// PerPrincipalUsage returns one row per (principal, currency) over
// [lower, upper). NULL principal columns (anonymous /
// auth-disabled rows) round-trip to an empty Principal string — the
// HTTP layer renders that as "anonymous" or similar, never as a
// fabricated identity.
func (r *sqlRepository) PerPrincipalUsage(ctx context.Context, lower, upper time.Time) ([]UsageBreakdown, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT
			COALESCE(principal, '') AS principal,
			currency,
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(estimated_cost_nano), 0)
		FROM llm_usage
		WHERE created_at >= ? AND created_at < ?
		GROUP BY principal, currency
		ORDER BY principal, currency`),
		lower.UTC().Format(TimestampLayout),
		upper.UTC().Format(TimestampLayout),
	)
	if err != nil {
		return nil, fmt.Errorf("llmusage: per-principal usage: %w", err)
	}
	defer rows.Close()
	var out []UsageBreakdown
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Principal, &b.Currency, &b.Calls, &b.InputTokens, &b.OutputTokens, &b.EstimatedCostNano); err != nil {
			return nil, fmt.Errorf("llmusage: scan per-principal usage: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("llmusage: iter per-principal usage: %w", err)
	}
	return out, nil
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
