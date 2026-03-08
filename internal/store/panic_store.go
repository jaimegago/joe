package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// sqlPanicStore implements safety.ClusterPanicStore against the
// cluster_panic_state table (created by migration 008).
// All joecored instances sharing the same SQLite file see the same row.
type sqlPanicStore struct {
	db     *sql.DB
	driver string
}

// NewPanicStore returns a safety.ClusterPanicStore backed by db.
func NewPanicStore(db *sql.DB, driver string) *sqlPanicStore {
	return &sqlPanicStore{db: db, driver: driver}
}

func (s *sqlPanicStore) SetPanicked(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, Rebind(s.driver, `
		UPDATE cluster_panic_state
		SET panicked=1, triggered_at=?
		WHERE id=1`),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("set cluster panic state: %w", err)
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
		return fmt.Errorf("clear cluster panic state: %w", err)
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
		return false, fmt.Errorf("read cluster panic state: %w", err)
	}
	return panicked == 1, nil
}
