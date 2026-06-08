package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// sqlRepository is the SQLite/Postgres-backed audit log. Only the two
// insert paths are exposed — Insert (opens its own statement directly on
// the database handle) and InsertTx (executes against a caller-supplied
// *sql.Tx so the audit row commits atomically with whatever settings or
// state mutation the caller is performing). Every other surface — UPDATE,
// DELETE, Truncate — is structurally absent. The underlying audit_log
// table has SQLite triggers (migration 015, preserved verbatim by 017)
// that ABORT any UPDATE or DELETE, so a future caller that bypassed this
// package could not mutate history either. The two enforcements are
// deliberately redundant.
type sqlRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository builds the SQL-backed audit Repository. The returned
// value implements ONLY Insert and InsertTx — there is no Update, Delete,
// Truncate, or any other mutator. Callers receive it via the Repository
// interface (audit.go) so the absence is structural, not just
// convention. The AST guard (audit_test.go) asserts exactly those two
// methods exist on the interface.
func NewRepository(db *sql.DB, driver string) Repository {
	return &sqlRepository{db: db, driver: driver}
}

// execContext is satisfied by both *sql.DB and *sql.Tx. The Insert and
// InsertTx methods funnel into one private body that runs against
// whichever the caller has — so the column defaulting, the empty-to-null
// mapping, and the SQL statement exist in EXACTLY ONE place and the two
// paths cannot drift apart.
type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Insert writes one audit row on its own connection. Equivalent behaviour
// to the original Phase F Insert: every existing caller is untouched.
func (r *sqlRepository) Insert(ctx context.Context, e Event) error {
	return r.insertOn(ctx, r.db, e)
}

// InsertTx writes one audit row against the caller-supplied transaction
// so the audit insert commits or rolls back atomically with whatever the
// caller's transaction is mutating. This is the lever the (later)
// settings service uses to make a settings change and its audit row a
// single durable event.
//
// The transaction is OWNED BY THE CALLER. InsertTx never calls Commit or
// Rollback; if the audit insert fails the caller is responsible for
// rolling back. A nil tx is a programming error and returns an error
// rather than silently falling back to the database handle — that
// fallback would defeat the point of the method (no shared transaction,
// no atomicity).
func (r *sqlRepository) InsertTx(ctx context.Context, tx *sql.Tx, e Event) error {
	if tx == nil {
		return fmt.Errorf("%w: nil transaction", ErrAuditWriteFailed)
	}
	return r.insertOn(ctx, tx, e)
}

// insertOn is the SINGLE place the audit-log INSERT lives. Both Insert
// and InsertTx delegate here. The SQL, the column defaulting, the
// empty-to-null mapping, and the kind/action validation all happen here
// — so the two paths produce identical rows for identical events.
func (r *sqlRepository) insertOn(ctx context.Context, exec execContext, e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Context == "" {
		e.Context = "{}"
	}
	if e.Decision == "" {
		// Default to deny: a row with an indeterminate decision is more
		// useful as a hard-deny audit entry than a silent shape error.
		e.Decision = DecisionDeny
	}
	if e.Kind == "" {
		// No safe default; refuse the row rather than miscategorise it.
		return fmt.Errorf("%w: kind required", ErrAuditWriteFailed)
	}
	if e.Action == "" {
		return fmt.Errorf("%w: action required", ErrAuditWriteFailed)
	}

	// principal/zone/component are nullable. Empty string → SQL NULL so the
	// CHECK constraints on the table behave consistently regardless of
	// what callers passed.
	var (
		principal sql.NullString
		zone      sql.NullString
		component sql.NullString
	)
	if e.Principal != "" {
		principal = sql.NullString{String: e.Principal, Valid: true}
	}
	if e.Zone != "" {
		zone = sql.NullString{String: e.Zone, Valid: true}
	}
	if e.ComponentID != "" {
		component = sql.NullString{String: e.ComponentID, Valid: true}
	}

	_, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO audit_log
			(created_at, principal, action, zone, component_id, decision, reason, kind, context)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		principal,
		e.Action,
		zone,
		component,
		string(e.Decision),
		e.Reason,
		string(e.Kind),
		e.Context,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuditWriteFailed, err)
	}
	return nil
}
