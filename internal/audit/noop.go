package audit

import (
	"context"
	"database/sql"
)

// NoopRepository discards every Insert call. Used by code paths that have
// no audit store wired (legacy tests, dev/local runs without a database).
// In production, cmd/joe-core/main.go always wires the SQL repository, so
// the accessor and transition sites get real audit rows.
type NoopRepository struct{}

// NewNoopRepository returns an audit Repository that accepts and discards
// every Insert. Errors are never returned; FailurePosture(nil, ...) is a
// no-op so the fail-open/fail-closed split is not exercised.
func NewNoopRepository() Repository { return NoopRepository{} }

// Insert always succeeds and writes nothing.
func (NoopRepository) Insert(_ context.Context, _ Event) error { return nil }

// InsertTx always succeeds and writes nothing. A nil tx is tolerated —
// the noop repository never touches it, so the SQL repository's
// "nil-tx is a programming error" rule does not apply here.
func (NoopRepository) InsertTx(_ context.Context, _ *sql.Tx, _ Event) error { return nil }
