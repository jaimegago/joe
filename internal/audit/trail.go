package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/store"
)

// ErrTrailReadFailed wraps a lower-level error so callers can identify a
// failed trail read without depending on the driver's error shape.
var ErrTrailReadFailed = errors.New("audit: trail read failed")

// TrailEntry is one act of a session's audit trail, as it is read back: the
// row's own columns beside the act its context records.
type TrailEntry struct {
	// ID is the row's identity, and the order of a trail is ascending ID: the
	// order the entries were written in.
	ID int64 `json:"id"`
	// RecordedAt is when the row was written, RFC 3339 UTC.
	RecordedAt string `json:"recorded_at"`
	Principal  string `json:"principal,omitempty"`
	// ComponentID is the managed system the act was taken against.
	ComponentID string `json:"component_id,omitempty"`
	Zone        string `json:"zone,omitempty"`
	// Decision is allow when the act was taken and deny when the accessor
	// refused it; DecisionReason is the policy engine's tag for either.
	Decision       Decision `json:"decision"`
	DecisionReason string   `json:"decision_reason,omitempty"`
	Act
}

// TrailReader is the audit log's one read surface: a session's act entries.
// It declares a single read and no write, and Repository declares no read, so
// holding one surface never grants the other.
type TrailReader interface {
	// SessionTrail returns the acts recorded for sessionID, in the order they
	// were written. A session that took no act returns an empty, non-nil
	// slice. The read mutates nothing: it writes no audit row of its own.
	SessionTrail(ctx context.Context, sessionID string) ([]TrailEntry, error)
}

// NewTrailReader builds the SQL-backed TrailReader.
func NewTrailReader(db *sql.DB, driver string) TrailReader {
	return &sqlRepository{db: db, driver: driver}
}

// SessionTrail reads a session's act entries in write order. Only
// infra_access rows carry a session_id (see Event.SessionID), so the kind
// predicate states the row shape the decode below relies on rather than
// narrowing anything.
//
// A row whose context does not decode as an Act fails the whole read. A trail
// that silently dropped the entry it could not parse would present a shorter
// history as the complete one.
func (r *sqlRepository) SessionTrail(ctx context.Context, sessionID string) ([]TrailEntry, error) {
	entries := []TrailEntry{}
	if sessionID == "" {
		return entries, nil
	}
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, created_at, principal, zone, component_id, decision, reason, context
		FROM audit_log
		WHERE session_id = ? AND kind = ?
		ORDER BY id ASC`), sessionID, string(KindInfraAccess))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTrailReadFailed, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			e                          TrailEntry
			principal, zone, component sql.NullString
			decision, blob             string
		)
		if err := rows.Scan(&e.ID, &e.RecordedAt, &principal, &zone, &component, &decision, &e.DecisionReason, &blob); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTrailReadFailed, err)
		}
		e.Principal = principal.String
		e.Zone = zone.String
		e.ComponentID = component.String
		e.Decision = Decision(decision)
		if err := json.Unmarshal([]byte(blob), &e.Act); err != nil {
			return nil, fmt.Errorf("%w: entry %d: undecodable act: %v", ErrTrailReadFailed, e.ID, err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTrailReadFailed, err)
	}
	return entries, nil
}
