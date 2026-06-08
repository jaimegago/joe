package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/safety"
)

// sqlPanicStore implements safety.ClusterPanicStore against the
// cluster_panic_state table (created by migration 008). It is the SINGLE home
// for panic state (D-0018 consolidation): panic entry writes this row and boot
// reads it — there is no panic.state file. The table holds exactly one row
// (id=1); all operations are UPDATEs.
type sqlPanicStore struct {
	db     *sql.DB
	driver string
}

// NewPanicStore returns a safety.ClusterPanicStore backed by db.
func NewPanicStore(db *sql.DB, driver string) *sqlPanicStore {
	return &sqlPanicStore{db: db, driver: driver}
}

// SetPanicked records an emergency shutdown in the single panic row, capturing
// who/when/why so boot logging and the panic status endpoint can report the
// trigger detail without a second store.
func (s *sqlPanicStore) SetPanicked(ctx context.Context, source safety.PanicSource, reason string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, Rebind(s.driver, `
		UPDATE cluster_panic_state
		SET panicked=1, triggered_at=?, trigger_source=?, trigger_reason=?
		WHERE id=1`),
		time.Now().UTC().Format(time.RFC3339),
		string(source),
		reason,
	)
	if err != nil {
		return fmt.Errorf("set panic state: %w", err)
	}
	return nil
}

func (s *sqlPanicStore) ClearPanicked(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, Rebind(s.driver, `
		UPDATE cluster_panic_state
		SET panicked=0, triggered_at=NULL, trigger_source=NULL, trigger_reason=NULL
		WHERE id=1`),
	)
	if err != nil {
		return fmt.Errorf("clear panic state: %w", err)
	}
	return nil
}

func (s *sqlPanicStore) IsPanicked(ctx context.Context) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	var panicked int
	err := s.db.QueryRowContext(ctx, `SELECT panicked FROM cluster_panic_state WHERE id=1`).Scan(&panicked)
	if err != nil {
		return false, fmt.Errorf("read panic state: %w", err)
	}
	return panicked == 1, nil
}

// PanicInfo returns the trigger detail of the single panic row, or (nil, nil)
// when the row is not in a panicked state (or the store has no db). It is the
// DB-row replacement for the deleted file reader.
func (s *sqlPanicStore) PanicInfo(ctx context.Context) (*safety.PanicInfo, error) {
	if s.db == nil {
		return nil, nil
	}
	var (
		panicked int
		at       sql.NullString
		source   sql.NullString
		reason   sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT panicked, triggered_at, trigger_source, trigger_reason FROM cluster_panic_state WHERE id=1`).
		Scan(&panicked, &at, &source, &reason)
	if err != nil {
		return nil, fmt.Errorf("read panic state: %w", err)
	}
	if panicked != 1 {
		return nil, nil
	}
	info := &safety.PanicInfo{
		TriggerSource: safety.PanicSource(source.String),
		TriggerReason: reason.String,
	}
	if at.Valid {
		if t, perr := time.Parse(time.RFC3339, at.String); perr == nil {
			info.TriggeredAt = t
		}
	}
	return info, nil
}
