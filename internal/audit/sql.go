package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// sqlRepository is the SQLite/Postgres-backed audit log. Insert is the only
// method on the interface; the type intentionally exposes nothing else. The
// underlying audit_log table has SQLite triggers (migration 015) that ABORT
// any UPDATE or DELETE, so even a future caller that bypassed this package
// could not mutate history.
type sqlRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository builds the SQL-backed audit Repository. The returned value
// implements ONLY Insert — there is no Update, Delete, Truncate, or any
// other mutator. Callers receive it via the Repository interface (audit.go)
// so the absence is structural, not just convention.
func NewRepository(db *sql.DB, driver string) Repository {
	return &sqlRepository{db: db, driver: driver}
}

func (r *sqlRepository) Insert(ctx context.Context, e Event) error {
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

	// principal/zone/source are nullable. Empty string → SQL NULL so the
	// CHECK constraints on the table behave consistently regardless of
	// what callers passed.
	var (
		principal sql.NullString
		zone      sql.NullString
		source    sql.NullString
	)
	if e.Principal != "" {
		principal = sql.NullString{String: e.Principal, Valid: true}
	}
	if e.Zone != "" {
		zone = sql.NullString{String: e.Zone, Valid: true}
	}
	if e.Source != "" {
		source = sql.NullString{String: e.Source, Valid: true}
	}

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO audit_log
			(created_at, principal, action, zone, source, decision, reason, kind, context)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		principal,
		e.Action,
		zone,
		source,
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
