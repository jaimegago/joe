// Package llmsettings owns the durable, operator-tunable controls for
// the LLM call path: the active model (singleton llm_settings table),
// the per-window cost thresholds (llm_cost_limits, three rows keyed by
// window name), and the session-lifetime token ceiling (singleton
// llm_runaway_limits).
//
// Stream G phase G4. Three pieces fit together here:
//
//  1. Repository — direct reads and writes against the three settings
//     tables. Every write is an UPDATE keyed by the singleton id or
//     the window name; the tables are pre-seeded at migration 017
//     time, so no INSERT path is needed.
//
//  2. Storage-backed providers — types that satisfy the existing
//     enforcement interfaces verbatim (agentloop.SessionLimits,
//     llmusage.CostLimits) by reading from the repository. Swapping
//     them in at construction sites is the only change the enforcement
//     check sites see; the call paths in agentloop.Agent.Run and
//     llmusage.RecorderAdapter.Chat are untouched.
//
//  3. MutationService — the SOLE write path. Every mutation runs in
//     one transaction: it reads the prior value, writes the new value,
//     and writes one settings-mutation audit row against the same
//     transaction via the audit repository's InsertTx. Either both
//     rows commit, or neither does. Audit context vocabulary —
//     "target", "before", "after" — is established here and becomes
//     the contract later admin readers depend on.
//
// Backstop policy: a stored value of zero or unset for a cost-window
// threshold or the runaway ceiling FALLS BACK to the conservative
// hardcoded backstop from the prior phases (llmusage.StaticCostLimits,
// agentloop.StaticSessionLimits) rather than meaning "no limit". A
// freshly migrated system whose tables still carry the migration-seeded
// zeros is therefore protected by the same backstop the prior phases
// installed, and an operator opts in to a different limit (including
// "off") by writing through the mutation service. This is the safer
// choice for launch; if a future operator workflow needs a true
// "unlimited" knob it should be an explicit negative or a new sentinel,
// not a silent reinterpretation of zero.
package llmsettings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// ErrSettingsWriteFailed wraps a lower-level error so callers can
// identify a settings-mutation failure without depending on the
// underlying driver error shape. Tests on the service exercise this via
// errors.Is.
var ErrSettingsWriteFailed = errors.New("llmsettings: write failed")

// Window names mirror the llmusage package's window vocabulary so the
// CHECK constraint in migration 017 and the runtime gate agree.
const (
	WindowHourly  = "hourly"
	WindowDaily   = "daily"
	WindowMonthly = "monthly"
)

// validWindow guards the three permitted window names. The CHECK on
// llm_cost_limits.window_name would reject anything else at the SQL
// layer, but failing in Go yields a clearer error.
func validWindow(name string) bool {
	switch name {
	case WindowHourly, WindowDaily, WindowMonthly:
		return true
	default:
		return false
	}
}

// CostLimitValues bundles the three per-window thresholds returned by
// Repository.ReadCostLimits in one call. Storing nano-unit integers
// matches the llm_usage.estimated_cost_nano scale and the cost-limit
// provider's interface.
type CostLimitValues struct {
	HourlyNano  int64
	DailyNano   int64
	MonthlyNano int64
}

// Repository is the read+update surface on the three settings tables.
// Reads are direct; writes are UPDATEs (the rows are pre-seeded at
// migration 017 time). A nil tx on an UpdateXxxTx is a programming
// error — every mutation lives inside the service's transaction.
type Repository interface {
	// ReadActiveModel returns the currently stored active model. An
	// empty string is a valid value (the migration seed) and signals
	// "not configured yet" to the startup precedence in main.go.
	ReadActiveModel(ctx context.Context) (string, error)
	// ReadActiveModelTx is the transactional read used by the mutation
	// service to capture the "before" value inside the same transaction
	// that will perform the write.
	ReadActiveModelTx(ctx context.Context, tx *sql.Tx) (string, error)
	// ReadCostLimits returns all three per-window thresholds in nano-
	// units of the configured currency. A zero per window means
	// "unset" — the storage-backed provider applies the documented
	// backstop fall-back for that window.
	ReadCostLimits(ctx context.Context) (CostLimitValues, error)
	// ReadCostLimitTx returns one window's stored threshold inside the
	// caller's transaction. Used by the mutation service to capture
	// the "before" value.
	ReadCostLimitTx(ctx context.Context, tx *sql.Tx, window string) (int64, error)
	// ReadRunawayCeiling returns the stored session token ceiling. A
	// zero value means "unset" — the storage-backed provider applies
	// the documented backstop fall-back.
	ReadRunawayCeiling(ctx context.Context) (int, error)
	// ReadRunawayCeilingTx is the transactional read counterpart used
	// by the mutation service.
	ReadRunawayCeilingTx(ctx context.Context, tx *sql.Tx) (int, error)

	// UpdateActiveModelTx persists the new active-model value inside
	// the caller's transaction. last_modified is stamped to the
	// supplied "now" so the test seam can fix a deterministic value
	// instead of pulling time.Now inside the repository.
	UpdateActiveModelTx(ctx context.Context, tx *sql.Tx, value string, now time.Time) error
	// UpdateCostLimitTx persists the new threshold for one window
	// inside the caller's transaction. The CHECK on window_name
	// catches any value not on the allowed list as a fallback to the
	// validWindow guard.
	UpdateCostLimitTx(ctx context.Context, tx *sql.Tx, window string, value int64, now time.Time) error
	// UpdateRunawayCeilingTx persists the new session token ceiling
	// inside the caller's transaction.
	UpdateRunawayCeilingTx(ctx context.Context, tx *sql.Tx, value int, now time.Time) error

	// DB exposes the underlying database handle so the mutation
	// service can BeginTx against it without re-importing *sql.DB.
	// Without this, the service would either need a second
	// constructor argument (the handle) or a separate "tx factory"
	// abstraction; one accessor on the repository keeps the seam
	// narrow.
	DB() *sql.DB
}

// NewRepository builds the SQL-backed Repository for the three settings
// tables.
func NewRepository(db *sql.DB, driver string) Repository {
	return &sqlRepository{db: db, driver: driver}
}

type sqlRepository struct {
	db     *sql.DB
	driver string
}

func (r *sqlRepository) DB() *sql.DB { return r.db }

func (r *sqlRepository) ReadActiveModel(ctx context.Context) (string, error) {
	var v string
	err := r.db.QueryRowContext(ctx,
		`SELECT active_model FROM llm_settings WHERE id = 1`,
	).Scan(&v)
	if err != nil {
		return "", fmt.Errorf("llmsettings: read active_model: %w", err)
	}
	return v, nil
}

func (r *sqlRepository) ReadActiveModelTx(ctx context.Context, tx *sql.Tx) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("%w: nil transaction", ErrSettingsWriteFailed)
	}
	var v string
	err := tx.QueryRowContext(ctx,
		`SELECT active_model FROM llm_settings WHERE id = 1`,
	).Scan(&v)
	if err != nil {
		return "", fmt.Errorf("llmsettings: read active_model (tx): %w", err)
	}
	return v, nil
}

func (r *sqlRepository) ReadCostLimits(ctx context.Context) (CostLimitValues, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT window_name, threshold FROM llm_cost_limits`,
	)
	if err != nil {
		return CostLimitValues{}, fmt.Errorf("llmsettings: read cost limits: %w", err)
	}
	defer rows.Close()
	var out CostLimitValues
	for rows.Next() {
		var name string
		var thr int64
		if err := rows.Scan(&name, &thr); err != nil {
			return CostLimitValues{}, fmt.Errorf("llmsettings: scan cost limit: %w", err)
		}
		switch name {
		case WindowHourly:
			out.HourlyNano = thr
		case WindowDaily:
			out.DailyNano = thr
		case WindowMonthly:
			out.MonthlyNano = thr
		}
	}
	if err := rows.Err(); err != nil {
		return CostLimitValues{}, fmt.Errorf("llmsettings: iter cost limits: %w", err)
	}
	return out, nil
}

func (r *sqlRepository) ReadCostLimitTx(ctx context.Context, tx *sql.Tx, window string) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("%w: nil transaction", ErrSettingsWriteFailed)
	}
	if !validWindow(window) {
		return 0, fmt.Errorf("%w: invalid window %q", ErrSettingsWriteFailed, window)
	}
	var v int64
	err := tx.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT threshold FROM llm_cost_limits WHERE window_name = ?`),
		window,
	).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("llmsettings: read cost limit (tx) %q: %w", window, err)
	}
	return v, nil
}

func (r *sqlRepository) ReadRunawayCeiling(ctx context.Context) (int, error) {
	var v int
	err := r.db.QueryRowContext(ctx,
		`SELECT session_token_ceiling FROM llm_runaway_limits WHERE id = 1`,
	).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("llmsettings: read runaway ceiling: %w", err)
	}
	return v, nil
}

func (r *sqlRepository) ReadRunawayCeilingTx(ctx context.Context, tx *sql.Tx) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("%w: nil transaction", ErrSettingsWriteFailed)
	}
	var v int
	err := tx.QueryRowContext(ctx,
		`SELECT session_token_ceiling FROM llm_runaway_limits WHERE id = 1`,
	).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("llmsettings: read runaway ceiling (tx): %w", err)
	}
	return v, nil
}

func (r *sqlRepository) UpdateActiveModelTx(ctx context.Context, tx *sql.Tx, value string, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrSettingsWriteFailed)
	}
	_, err := tx.ExecContext(ctx, store.Rebind(r.driver,
		`UPDATE llm_settings SET active_model = ?, last_modified = ? WHERE id = 1`),
		value, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("%w: update active_model: %v", ErrSettingsWriteFailed, err)
	}
	return nil
}

func (r *sqlRepository) UpdateCostLimitTx(ctx context.Context, tx *sql.Tx, window string, value int64, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrSettingsWriteFailed)
	}
	if !validWindow(window) {
		return fmt.Errorf("%w: invalid window %q", ErrSettingsWriteFailed, window)
	}
	_, err := tx.ExecContext(ctx, store.Rebind(r.driver,
		`UPDATE llm_cost_limits SET threshold = ?, last_modified = ? WHERE window_name = ?`),
		value, now.UTC().Format(time.RFC3339Nano), window)
	if err != nil {
		return fmt.Errorf("%w: update cost limit %q: %v", ErrSettingsWriteFailed, window, err)
	}
	return nil
}

func (r *sqlRepository) UpdateRunawayCeilingTx(ctx context.Context, tx *sql.Tx, value int, now time.Time) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrSettingsWriteFailed)
	}
	_, err := tx.ExecContext(ctx, store.Rebind(r.driver,
		`UPDATE llm_runaway_limits SET session_token_ceiling = ?, last_modified = ? WHERE id = 1`),
		value, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("%w: update runaway ceiling: %v", ErrSettingsWriteFailed, err)
	}
	return nil
}
