package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/store"
)

// ErrNotAppendOnly is returned by VerifyAppendOnly when the database accepted
// an UPDATE or a DELETE against audit_log, or when it could not be shown to
// have refused one.
var ErrNotAppendOnly = errors.New("audit: audit_log is not append-only on this store")

// probeAction marks the row VerifyAppendOnly writes and rolls back. It never
// persists, so it is no audit verb and appears in no vocabulary.
const probeAction = "audit.append_only_probe"

// VerifyAppendOnly proves, against the database Joe actually opened, that
// audit_log refuses UPDATE and DELETE. Joe calls it once at boot and does not
// serve when it fails (D-0171).
//
// It is a behavioural probe, not a catalogue query: it asks the database to
// change a row and requires the database to refuse, so it reads the same on
// every SQL store and does not depend on how a dialect spells or stores a
// trigger. Inside one transaction it writes a probe row, attempts the
// UPDATE, requires an error and requires the row to still read as written,
// attempts the DELETE under the same two requirements, and rolls back. The
// probe row is never committed.
//
// The row must be shown intact after each refusal, and that is what makes an
// error count: a statement that failed because the connection dropped, or the
// context ended, leaves a transaction in which the confirming read fails too,
// and the probe then reports that it could not establish the property. Only
// "the statement errored and the row is unchanged" passes.
func VerifyAppendOnly(ctx context.Context, db *sql.DB, driver string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin probe: %v", ErrNotAppendOnly, err)
	}
	// The probe never commits, whatever happens.
	defer func() { _ = tx.Rollback() }()

	const marker = "append_only_probe"
	r := &sqlRepository{db: db, driver: driver}
	if err := r.insertOn(ctx, tx, Event{
		Action:   probeAction,
		Decision: DecisionDeny,
		Reason:   marker,
		Kind:     KindInfraAccess,
	}); err != nil {
		return fmt.Errorf("%w: write probe row: %v", ErrNotAppendOnly, err)
	}

	// The probe row is the newest row carrying the marker action, so walking
	// the primary key backwards finds it at once however large the table is;
	// every statement after this one addresses it by id.
	var id int64
	if err := tx.QueryRowContext(ctx, store.Rebind(driver,
		`SELECT id FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`), probeAction).Scan(&id); err != nil {
		return fmt.Errorf("%w: locate probe row: %v", ErrNotAppendOnly, err)
	}

	attempts := []struct {
		name string
		stmt string
		args []any
	}{
		{"UPDATE", `UPDATE audit_log SET reason = ? WHERE id = ?`, []any{"tampered", id}},
		{"DELETE", `DELETE FROM audit_log WHERE id = ?`, []any{id}},
	}
	for _, a := range attempts {
		// A savepoint per attempt: a refused statement aborts the whole
		// transaction on some stores, and rolling back to the savepoint is
		// what lets the confirming read run on all of them.
		if _, err := tx.ExecContext(ctx, `SAVEPOINT append_only_probe`); err != nil {
			return fmt.Errorf("%w: %s probe: savepoint: %v", ErrNotAppendOnly, a.name, err)
		}
		_, execErr := tx.ExecContext(ctx, store.Rebind(driver, a.stmt), a.args...)
		if execErr == nil {
			return fmt.Errorf("%w: the database accepted %s on audit_log", ErrNotAppendOnly, a.name)
		}
		if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT append_only_probe`); err != nil {
			return fmt.Errorf("%w: %s probe: could not confirm the refusal (%v): %v", ErrNotAppendOnly, a.name, execErr, err)
		}
		var reason string
		if err := tx.QueryRowContext(ctx, store.Rebind(driver,
			`SELECT reason FROM audit_log WHERE id = ?`), id).Scan(&reason); err != nil {
			return fmt.Errorf("%w: %s probe: probe row unreadable after the attempt (%v): %v", ErrNotAppendOnly, a.name, execErr, err)
		}
		if reason != marker {
			return fmt.Errorf("%w: %s changed the probe row", ErrNotAppendOnly, a.name)
		}
	}
	return nil
}
